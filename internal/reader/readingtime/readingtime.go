// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package readingtime provides a function to estimate the reading time of an article.
package readingtime

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/proxyrotator"
	"miniflux.app/v2/internal/reader/fetcher"
	"miniflux.app/v2/internal/reader/sanitizer"
)

// textReadingTime returns the estimated reading time of a text in minutes.
func textReadingTime(content string, defaultReadingSpeed, cjkReadingSpeed int) int {
	const truncationPoint = 100

	sanitizedContent := sanitizer.StripTags(content)

	if isCJK(sanitizedContent, truncationPoint) {
		return (utf8.RuneCountInString(sanitizedContent) + cjkReadingSpeed - 1) / cjkReadingSpeed
	}

	return (countWords(sanitizedContent) + defaultReadingSpeed - 1) / defaultReadingSpeed
}

func countWords(s string) int {
	n := 0
	for range strings.FieldsSeq(s) {
		n++
	}
	return n
}

func isCJK(text string, limit int) bool {
	var letters, totalCJK int
	for _, r := range text {
		// Numbers and control characters often used in CJK too.
		// Counting them makes detection less reliable.
		if !unicode.In(r, unicode.Letter) {
			continue
		}

		if letters++; letters == limit {
			break
		}

		if unicode.In(r, unicode.Han, unicode.Hangul, unicode.Hiragana, unicode.Katakana, unicode.Yi, unicode.Bopomofo) {
			totalCJK++
		}
	}

	// If at least half of the letters is CJK, odds are that the text is CJK.
	midpoint := letters / 2

	return totalCJK > midpoint
}

func fetchWatchTime(websiteURL, query string, isoDate bool) (int, error) {
	requestBuilder := fetcher.NewRequestBuilder().
		WithTimeout(config.Opts.HTTPClientTimeout()).
		WithProxyRotator(proxyrotator.ProxyRotatorInstance)

	responseHandler := fetcher.NewResponseHandler(requestBuilder.ExecuteRequest(websiteURL))
	defer responseHandler.Close()

	if localizedError := responseHandler.LocalizedError(); localizedError != nil {
		slog.Warn("Unable to fetch watch time", slog.String("website_url", websiteURL), slog.Any("error", localizedError.Error()))
		return 0, localizedError.Error()
	}

	doc, docErr := goquery.NewDocumentFromReader(responseHandler.Body(config.Opts.HTTPClientMaxBodySize()))
	if docErr != nil {
		return 0, docErr
	}

	duration, exists := doc.FindMatcher(goquery.Single(query)).Attr("content")
	if !exists {
		return 0, errors.New("duration not found")
	}

	ret := 0
	if isoDate {
		parsedDuration, err := parseISO8601Duration(duration)
		if err != nil {
			return 0, fmt.Errorf("unable to parse iso duration %s: %v", duration, err)
		}
		ret = int(parsedDuration.Minutes())
	} else {
		parsedDuration, err := strconv.Atoi(duration)
		if err != nil {
			return 0, fmt.Errorf("unable to parse duration %s: %v", duration, err)
		}
		ret = parsedDuration / 60
	}
	return ret, nil
}

// EstimateReadingTime returns the reading time of an entry in minutes.
// For video entries, the watch time is fetched from the platform unless the
// entry already has a reading time. Otherwise, it is estimated from the content.
func EstimateReadingTime(entry *model.Entry, user *model.User) int {
	if !user.ShowReadingTime {
		slog.Debug("Skip reading time estimation for this user", slog.Int64("user_id", user.ID))
		return 0
	}

	// Define watch time fetching scenarios
	watchTimeScenarios := [...]struct {
		shouldFetch func(*model.Entry) bool
		fetchFunc   func(string) (int, error)
		platform    string
	}{
		{shouldFetchYouTubeWatchTimeForSingleEntry, fetchYouTubeWatchTimeForSingleEntry, "YouTube"},
		{shouldFetchNebulaWatchTime, fetchNebulaWatchTime, "Nebula"},
		{shouldFetchOdyseeWatchTime, fetchOdyseeWatchTime, "Odysee"},
		{shouldFetchBilibiliWatchTime, fetchBilibiliWatchTime, "Bilibili"},
	}

	// Iterate through scenarios and attempt to fetch watch time
	for _, scenario := range watchTimeScenarios {
		if scenario.shouldFetch(entry) {
			if entry.ReadingTime > 0 {
				return entry.ReadingTime
			}

			watchTime, err := scenario.fetchFunc(entry.URL)
			if err != nil {
				slog.Warn("Unable to fetch watch time",
					slog.String("platform", scenario.platform),
					slog.Int64("user_id", user.ID),
					slog.Int64("entry_id", entry.ID),
					slog.String("entry_url", entry.URL),
					slog.Any("error", err),
				)
			} else if watchTime > 0 {
				return watchTime
			}
			break
		}
	}

	// Fallback to text-based reading time estimation
	return textReadingTime(entry.Content, user.DefaultReadingSpeed, user.CJKReadingSpeed)
}

// parseISO8601Duration parses a subset of ISO8601 durations, mainly for youtube video.
func parseISO8601Duration(duration string) (time.Duration, error) {
	after, ok := strings.CutPrefix(duration, "PT")
	if !ok {
		return 0, errors.New("the period doesn't start with PT")
	}

	var d time.Duration
	start := 0

	for i := 0; i < len(after); i++ {
		var unit time.Duration

		switch after[i] {
		case 'Y', 'W', 'D':
			return 0, fmt.Errorf("the '%c' specifier isn't supported", after[i])
		case 'H':
			unit = time.Hour
		case 'M':
			unit = time.Minute
		case 'S':
			unit = time.Second
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			continue
		default:
			return 0, errors.New("invalid character in the period")
		}

		val, err := strconv.Atoi(after[start:i])
		if err != nil {
			return 0, err
		}
		d += time.Duration(val) * unit
		start = i + 1
	}
	return d, nil
}
