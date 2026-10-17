package ui

import (
	"cmp"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/crypto"
	"miniflux.app/v2/internal/http/request"
	"miniflux.app/v2/internal/http/response"
	"miniflux.app/v2/internal/locale"
	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/reader/processor"
	"miniflux.app/v2/internal/ui/form"
	"miniflux.app/v2/internal/ui/view"
)

func (h *handler) submitEntry(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}
	printer := locale.NewPrinter(user.Language)

	queryBuilder := h.store.NewFeedQueryBuilder(user.ID)
	queryBuilder.WithManual(true)
	feeds, err := queryBuilder.GetFeeds()
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	v := view.New(h.tpl, r)
	v.Set("feeds", feeds)
	v.Set("menu", "search")
	v.Set("user", user)
	navMetadata, _ := h.store.GetNavMetadata(user.ID)
	v.Set("countUnread", navMetadata.CountUnread)
	v.Set("countErrorFeeds", navMetadata.CountErrorFeeds)
	v.Set("defaultUserAgent", config.Opts.HTTPClientUserAgent())
	v.Set("hasProxyConfigured", config.Opts.HasHTTPClientProxyURLConfigured())

	entryForm := form.NewEntryForm(r)
	if validationErr := entryForm.Validate(); validationErr != nil {
		v.Set("form", entryForm)
		v.Set("errorMessage", validationErr.Translate(user.Language))
		response.HTML(w, r, v.Render("add_entry"))
		return
	}

	var date time.Time
	if entryForm.Date != "" {
		date, err = time.Parse(time.DateOnly, entryForm.Date)
		if err != nil {
			response.HTMLServerError(w, r, err)
			return
		}
	}

	entry := model.NewEntry()
	entry.Title = entryForm.Title
	entry.URL = entryForm.URL
	entry.Hash = crypto.SHA256(entry.URL)
	entry.UserID = user.ID
	entry.Date = cmp.Or(date, time.Now())
	entry.Tags = model.CleanTags(entryForm.Tags)
	entry.CreatedAt = time.Now()
	entry.ChangedAt = time.Now()

	if entryForm.FeedID == 0 {
		title := printer.Print("feed.manual")
		if len(feeds) > 0 {
			title += " " + strconv.FormatInt(int64(len(feeds))+1, 10)
		}

		feedUniqueString := fmt.Sprintf("manual-%d", time.Now().Unix())
		newFeed := &model.Feed{
			Manual:   true,
			Disabled: true,
			Crawler:  true,
			UserID:   entry.UserID,
			Title:    title,

			// These are not relevant but must be unique
			FeedURL: feedUniqueString,
			SiteURL: feedUniqueString,
		}

		err = h.store.CreateFeed(newFeed)
		if err != nil {
			v.Set("form", entryForm)
			v.Set("errorMessage", err.Error()) // TODO: localize error message
			response.HTML(w, r, v.Render("add_entry"))
			return
		}

		entry.FeedID = newFeed.ID
		entry.Feed = newFeed

	} else {
		queryBuilder := h.store.NewFeedQueryBuilder(user.ID)
		queryBuilder.WithManual(true)
		queryBuilder.WithFeedID(entryForm.FeedID)
		feed, err := queryBuilder.GetFeed()
		if err != nil {
			response.HTMLServerError(w, r, err)
			return
		}
		entry.FeedID = feed.ID
		entry.Feed = feed
	}

	if entryForm.Download {
		// Proccess the entry now to fill fields
		if err := processor.ProcessEntryWebPage(entry.Feed, entry, user); err != nil {
			response.HTMLServerError(w, r, err)
			return
		}
	}

	if err := h.store.CreateEntry(entry); err != nil {
		v.Set("form", entryForm)
		v.Set("errorMessage", err.Error()) // TODO: localize error message
		response.HTML(w, r, v.Render("add_entry"))
		return
	}

	response.HTMLRedirect(w, r, h.routePath("/feed/%d/entry/%d", entry.FeedID, entry.ID))
}
