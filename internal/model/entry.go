// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package model // import "miniflux.app/v2/internal/model"

import (
	"slices"
	"strings"
	"time"
)

// Entry statuses and default sorting order.
const (
	EntryStatusUnread       = "unread"
	EntryStatusRead         = "read"
	DefaultSortingOrder     = "published_at"
	DefaultSortingDirection = "asc"
)

// MaxEntryLimit is the maximum allowed value for the "limit" query parameter
// and for the user "entries_per_page" preference.
const MaxEntryLimit = 1000

// MaxEntryIDsLimit is the maximum allowed value for the "limit" query parameter
// for the entry ID list endpoints.
const MaxEntryIDsLimit = 10000

// Entry represents a feed item in the system.
type Entry struct {
	ID          int64         `json:"id"`
	UserID      int64         `json:"user_id"`
	FeedID      int64         `json:"feed_id"`
	Status      string        `json:"status"`
	Hash        string        `json:"hash"`
	Title       string        `json:"title"`
	URL         string        `json:"url"`
	CommentsURL string        `json:"comments_url"`
	Language    string        `json:"language"`
	Date        time.Time     `json:"published_at"`
	CreatedAt   time.Time     `json:"created_at"`
	ChangedAt   time.Time     `json:"changed_at"`
	Content     string        `json:"content"`
	Author      string        `json:"author"`
	ShareCode   string        `json:"share_code"`
	Starred     bool          `json:"starred"`
	ReadingTime int           `json:"reading_time"`
	Enclosures  EnclosureList `json:"enclosures"`
	Feed        *Feed         `json:"feed,omitempty"`
	Tags        []string      `json:"tags"`
}

func NewEntry() *Entry {
	return &Entry{
		Enclosures: make(EnclosureList, 0),
		Tags:       make([]string, 0),
		Feed: &Feed{
			Icon: &FeedIcon{},
		},
	}
}

// ShouldMarkAsReadOnView Return whether the entry should be marked as viewed considering all user settings and entry state.
func (e *Entry) ShouldMarkAsReadOnView(user *User) bool {
	// Already read, no need to mark as read again. Removed entries are not marked as read
	if e.Status != EntryStatusUnread {
		return false
	}

	// There is an enclosure, markAsRead will happen at enclosure completion time, no need to mark as read on view
	if user.MarkReadOnMediaPlayerCompletion && e.Enclosures.ContainsAudioOrVideo() {
		return false
	}

	// The user wants to mark as read on view
	return user.MarkReadOnView
}

// Entries represents a list of entries.
type Entries []*Entry

// EntriesStatusUpdateRequest represents a request to change entries status.
type EntriesStatusUpdateRequest struct {
	EntryIDs []int64 `json:"entry_ids"`
	Status   string  `json:"status"`
	Starred  *bool   `json:"starred"`
}

// EntryUpdateRequest represents a request to update an entry.
type EntryUpdateRequest struct {
	URL           *string    `json:"url"`
	Title         *string    `json:"title"`
	Author        *string    `json:"author"`
	Content       *string    `json:"content"`
	ContentHTML   *string    `json:"content_html"`
	Date          *time.Time `json:"date"`
	ReadingTime   *int       `json:"reading_time"`
	Tags          []string   `json:"tags"`
	ScrollPercent *float32   `json:"scroll_percent"`
}

func (e *EntryUpdateRequest) Patch(entry *Entry) {
	if e.URL != nil && *e.URL != "" {
		entry.URL = *e.URL
	}

	if e.Title != nil && *e.Title != "" {
		entry.Title = *e.Title
	}

	if e.Author != nil && *e.Author != "" {
		entry.Author = *e.Author
	}

	if e.Date != nil && !e.Date.IsZero() {
		entry.Date = *e.Date
	}

	if e.Content != nil && *e.Content != "" {
		entry.Content = *e.Content
	}

	if e.ReadingTime != nil && *e.ReadingTime != 0 {
		entry.ReadingTime = *e.ReadingTime
	}

	if e.Date != nil && !e.Date.IsZero() {
		entry.Date = *e.Date
	}

	if e.Tags != nil {
		entry.Tags = CleanTags(e.Tags)
	}

	if e.ScrollPercent != nil && *e.ScrollPercent != 0.0 {
		entry.ScrollPercent = *e.ScrollPercent
	}
}

func CleanTags(tags []string) []string {
	cleanedTags := make([]string, 0, len(tags))
	for _, tag := range tags {
		cleanedTag := strings.ToLower(strings.TrimSpace(tag))
		if cleanedTag != "" {
			cleanedTags = append(cleanedTags, cleanedTag)
		}
	}
	slices.Sort(cleanedTags)
	return slices.Compact(cleanedTags)
}
