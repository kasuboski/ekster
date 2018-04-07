package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// HubBackend handles information for the incoming handler
type HubBackend interface {
	CreateFeed(url string, contentType string) (int64, error)
	GetSecret(id int64) string
	UpdateFeed(feedID int64, contentType string, content []byte) error
}

type incomingHandler struct {
	Backend HubBackend
}

var (
	urlRegex = regexp.MustCompile(`^/incoming/(\d+)$`)
)

func (h *incomingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	r.ParseForm()
	log.Printf("%s %s\n", r.Method, r.URL)
	log.Println(r.URL.Query())
	log.Println(r.PostForm)

	if r.Method == http.MethodGet {
		values := r.URL.Query()

		// check

		verify := values.Get("hub.challenge")
		fmt.Fprint(w, verify)

		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}

	// find feed
	matches := urlRegex.FindStringSubmatch(r.URL.Path)
	feed, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		fmt.Fprint(w, err)
	}

	// find secret
	secret := h.Backend.GetSecret(feed)
	if secret == "" {
		http.Error(w, "Unknown", 400)
		return
	}

	// match signature
	sig := r.Header.Get("X-Hub-Signature")
	parts := strings.Split(sig, "=")

	if len(parts) != 2 {
		http.Error(w, "Signature format", 400)
		return
	}

	if sig != "sha1" {
		http.Error(w, "Unknown signature format", 400)
		return
	}

	feedContent, err := ioutil.ReadAll(r.Body)

	// verification
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write(feedContent)
	signature := mac.Sum(nil)

	if fmt.Sprintf("%x", signature) != parts[1] {
		http.Error(w, "Signature doesn't match", 400)
		return
	}

	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/rss+xml") {
		// RSS parsing
		h.Backend.UpdateFeed(feed, ct, feedContent)
	} else if strings.HasPrefix(ct, "application/atom+xml") {
		// Atom parsing
		h.Backend.UpdateFeed(feed, ct, feedContent)
	} else if strings.HasPrefix(ct, "text/html") {
		// h-entry parsing
		h.Backend.UpdateFeed(feed, ct, feedContent)
	} else {
		http.Error(w, fmt.Sprintf("Unknown format of body: %s", ct), 400)
		return
	}

	return
}