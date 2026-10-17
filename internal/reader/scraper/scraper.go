// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package scraper // import "miniflux.app/v2/internal/reader/scraper"

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/reader/encoding"
	"miniflux.app/v2/internal/reader/fetcher"
	"miniflux.app/v2/internal/reader/readability"
	"miniflux.app/v2/internal/urllib"

	"github.com/PuerkitoBio/goquery"
)

func ScrapeWebsite(requestBuilder *fetcher.RequestBuilder, pageURL, rules string) (request model.EntryUpdateRequest, err error) {
	responseHandler := fetcher.NewResponseHandler(requestBuilder.ExecuteRequest(pageURL))
	defer responseHandler.Close()

	if localizedError := responseHandler.LocalizedError(); localizedError != nil {
		slog.Warn("Unable to scrape website", slog.String("website_url", pageURL), slog.Any("error", localizedError.Error()))
		return request, localizedError.Error()
	}

	if !isAllowedContentType(responseHandler.ContentType()) {
		return request, fmt.Errorf("scraper: this resource is not a HTML document (%s)", responseHandler.ContentType())
	}

	// The entry URL could redirect somewhere else.
	sameSite := urllib.Domain(pageURL) == urllib.Domain(responseHandler.EffectiveURL())
	pageURL = responseHandler.EffectiveURL()

	if rules == "" {
		rules = getPredefinedScraperRules(pageURL)
	}

	htmlDocumentReader, err := encoding.NewCharsetReader(
		responseHandler.Body(config.Opts.HTTPClientMaxBodySize()),
		responseHandler.ContentType(),
	)

	// Parse the document once up front and use it multiple times
	document, err := goquery.NewDocumentFromReader(htmlDocumentReader)
	if err != nil {
		return request, err
	}

	if err != nil {
		return request, fmt.Errorf("scraper: unable to read HTML document with charset reader: %v", err)
	}

	if sameSite {
		if rules != "" {
			slog.Debug("Extracting content with custom rules",
				"url", pageURL,
				"rules", rules,
			)
			baseURL, extractedContent, err := findContentUsingCustomRules(document, rules)
			if err != nil {
				return request, err
			}
			request.URL = &baseURL
			request.Content = &extractedContent
		} else {
			slog.Debug("Extracting content with readability",
				"url", pageURL,
			)
			request, err = readability.ExtractContentFromDocument(document)
			if err != nil {
				return request, err
			}
		}
	}

	// Pull and set the entry's metadata from the document
	// Most of the data can be extracted from the document's head section
	// from standard HTML tags and meta properties or OpenGraph tags.
	title := trySelectorAttrs(document, map[string]string{
		"head meta[property='og:title']": "content",
		"head title":                     "",
	})
	if title != "" {
		request.Title = model.OptionalString(title)
	}

	author := trySelectorAttrs(document, map[string]string{
		"head meta[property='article:author']": "content",
		"a rel=author":                         "",
	})
	if author != "" {
		request.Author = model.OptionalString(author)
	}

	date := trySelectorAttrs(document, map[string]string{
		"head meta[property='article:modified_time']":  "content",
		"head meta[property='article:published_time']": "content",
		"head meta[property='pubdate']":                "content",
		"head meta[property='date']":                   "content",
	})
	if date == "" {
		date = responseHandler.LastModified()
	}
	request.Date = tryParseDate(date)

	if request.Content == nil || *request.Content == "" {
		desc := trySelectorAttrs(document, map[string]string{
			"head meta[property='og:description']": "content",
			"head meta[name='description']":        "content",
		})
		if desc != "" {
			request.Content = &desc
		}
	}

	rawTags := trySelectorAttrs(document, map[string]string{
		"head meta[property='article:tag']": "content",
		"head meta[name='keywords']":        "content",
	})
	tags := model.CleanTags(strings.Split(rawTags, ","))
	if len(tags) > 0 {
		request.Tags = tags
	}

	urlValue := trySelectorAttrs(document, map[string]string{"head base": "href", "meta[property='og:url']": "content"})

	urlValue = strings.TrimSpace(urlValue)
	if urllib.IsAbsoluteURL(urlValue) {
		request.URL = &urlValue
	}

	if request.URL == nil || *request.URL == "" {
		request.URL = &pageURL
	} else {
		slog.Debug("Using base URL from HTML document", "base_url", request.URL)
	}

	return request, nil
}

func findContentUsingCustomRules(document *goquery.Document, rules string) (baseURL string, extractedContent string, err error) {
	if hrefValue, exists := document.FindMatcher(goquery.Single("head base")).Attr("href"); exists {
		hrefValue = strings.TrimSpace(hrefValue)
		if urllib.IsAbsoluteURL(hrefValue) {
			baseURL = hrefValue
		}
	}

	var buf strings.Builder
	document.Find(rules).Each(func(i int, s *goquery.Selection) {
		if content, err := goquery.OuterHtml(s); err == nil {
			buf.WriteString(content)
		}
	})

	return baseURL, buf.String(), nil
}

func getPredefinedScraperRules(websiteURL string) string {
	urlDomain := urllib.DomainWithoutWWW(websiteURL)

	if rules, ok := predefinedRules[urlDomain]; ok {
		return rules
	}
	return ""
}

func isAllowedContentType(contentType string) bool {
	contentType = strings.ToLower(contentType)
	return strings.HasPrefix(contentType, "text/html") ||
		strings.HasPrefix(contentType, "application/xhtml+xml")
}

func trySelectorAttrs(document *goquery.Document, selectors map[string]string) string {
	for selector, attr := range selectors {
		sel := document.FindMatcher(goquery.Single(selector))
		if attr == "" {
			return strings.TrimSpace(sel.Text())
		}
		if value, exists := sel.Attr(attr); exists {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func tryParseDate(input string) *time.Time {
	if input == "" {
		return nil
	}

	// https://pkg.go.dev/time#pkg-constants
	formats := []string{
		time.RFC1123, // Should be correct for the Last-Modified header
		time.RFC3339,
		time.DateOnly,
		time.UnixDate,
		time.RFC822,
		time.RFC850,
		time.ANSIC,
		time.RubyDate,
		time.Layout,
	}

	for _, format := range formats {
		if parsedDate, err := time.Parse(format, input); err == nil {
			return &parsedDate
		}
	}

	// If nothing worked try parsing as Unix timestamp
	if i, err := strconv.ParseInt(input, 10, 64); err == nil {
		return new(time.Unix(i, 0))
	}

	return nil
}
