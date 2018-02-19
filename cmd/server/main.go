package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/pstuifzand/microsub-server/microsub"
	"willnorris.com/go/microformats"
)

var port int

func init() {
	flag.IntVar(&port, "port", 80, "port for serving api")
}

type microsubHandler struct {
	Backend microsub.Microsub
}

func simplify(item map[string][]interface{}) map[string]interface{} {
	feedItem := make(map[string]interface{})

	for k, v := range item {
		if k == "bookmark-of" || k == "like-of" || k == "repost-of" || k == "in-reply-to" {
			if value, ok := v[0].(*microformats.Microformat); ok {
				m := simplify(value.Properties)
				m["type"] = value.Type[0][2:]
				feedItem[k] = []interface{}{m}
			} else {
				feedItem[k] = v
			}
		} else if k == "content" {
			if content, ok := v[0].(map[string]interface{}); ok {
				if text, e := content["value"]; e {
					delete(content, "value")
					if _, e := content["html"]; !e {
						content["text"] = text
					}
				}
				feedItem[k] = content
			}
		} else if k == "photo" {
			feedItem[k] = v
		} else if k == "video" {
			feedItem[k] = v
		} else if k == "featured" {
			feedItem[k] = v
		} else if value, ok := v[0].(*microformats.Microformat); ok {
			m := simplify(value.Properties)
			m["type"] = value.Type[0][2:]
			feedItem[k] = m
		} else if value, ok := v[0].(string); ok {
			feedItem[k] = value
		} else if value, ok := v[0].(map[string]interface{}); ok {
			feedItem[k] = value
		} else if value, ok := v[0].([]interface{}); ok {
			feedItem[k] = value
		}
	}
	return feedItem
}

func simplifyMicroformat(item *microformats.Microformat) map[string]interface{} {
	newItem := simplify(item.Properties)
	newItem["type"] = item.Type[0][2:]

	children := []map[string]interface{}{}

	if len(item.Children) > 0 {
		for _, c := range item.Children {
			child := simplifyMicroformat(c)
			if c, e := child["children"]; e {
				if ar, ok := c.([]map[string]interface{}); ok {
					children = append(children, ar...)
				}
				delete(child, "children")
			}
			children = append(children, child)
		}

		newItem["children"] = children
	}

	return newItem
}

func simplifyMicroformatData(md *microformats.Data) []map[string]interface{} {
	items := []map[string]interface{}{}
	for _, item := range md.Items {
		newItem := simplifyMicroformat(item)
		items = append(items, newItem)
		if c, e := newItem["children"]; e {
			if ar, ok := c.([]map[string]interface{}); ok {
				items = append(items, ar...)
			}
			delete(newItem, "children")
		}
	}
	return items
}

func (h *microsubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Println(r.URL.String())

	if r.Method == http.MethodGet {
		values := r.URL.Query()
		action := values.Get("action")
		if action == "channels" {
			channels := h.Backend.ChannelsGetList()
			jw := json.NewEncoder(w)
			w.Header().Add("Content-Type", "application/json")
			jw.Encode(map[string][]microsub.Channel{
				"channels": channels,
			})
		} else if action == "timeline" {
			timeline := h.Backend.TimelineGet(values.Get("after"), values.Get("before"), values.Get("channel"))
			jw := json.NewEncoder(w)
			w.Header().Add("Content-Type", "application/json")
			jw.SetIndent("", "    ")
			jw.Encode(timeline)
		} else if action == "preview" {
			md, err := Fetch2(values.Get("url"))
			if err != nil {
				http.Error(w, "Failed parsing url", 500)
				return
			}

			results := simplifyMicroformatData(md)

			jw := json.NewEncoder(w)
			jw.SetIndent("", "    ")
			w.Header().Add("Content-Type", "application/json")
			jw.Encode(map[string]interface{}{
				"items":  results,
				"paging": microsub.Pagination{},
			})
		} else if action == "follow" {
			channel := values.Get("channel")
			following := h.Backend.FollowGetList(channel)
			jw := json.NewEncoder(w)
			w.Header().Add("Content-Type", "application/json")
			jw.Encode(map[string][]microsub.Feed{
				"items": following,
			})
		}
		return
	} else if r.Method == http.MethodPost {
		values := r.URL.Query()
		action := values.Get("action")
		if action == "channels" {
			name := values.Get("name")
			method := values.Get("method")
			uid := values.Get("channel")
			if method == "delete" {
				h.Backend.ChannelsDelete(uid)
				w.Header().Add("Content-Type", "application/json")
				fmt.Fprintln(w, "[]")
				h.Backend.(Debug).Debug()
				return
			}

			jw := json.NewEncoder(w)
			if uid == "" {
				channel := h.Backend.ChannelsCreate(name)
				w.Header().Add("Content-Type", "application/json")
				jw.Encode(channel)
			} else {
				channel := h.Backend.ChannelsUpdate(uid, name)
				w.Header().Add("Content-Type", "application/json")
				jw.Encode(channel)
			}
			h.Backend.(Debug).Debug()
		} else if action == "follow" {
			uid := values.Get("channel")
			url := values.Get("url")

			feed := h.Backend.FollowURL(uid, url)
			w.Header().Add("Content-Type", "application/json")
			jw := json.NewEncoder(w)
			jw.Encode(feed)
		} else if action == "unfollow" {
			uid := values.Get("channel")
			url := values.Get("url")
			h.Backend.UnfollowURL(uid, url)
			w.Header().Add("Content-Type", "application/json")
			fmt.Fprintln(w, "[]")
		} else if action == "search" {
			query := values.Get("query")
			feeds := h.Backend.Search(query)
			jw := json.NewEncoder(w)
			w.Header().Add("Content-Type", "application/json")
			jw.Encode(map[string][]microsub.Feed{
				"results": feeds,
			})
		}
		return
	}
	return
}

func main() {
	flag.Parse()

	createBackend := false
	args := flag.Args()

	if len(args) >= 1 {
		if args[0] == "new" {
			createBackend = true
		}
	}

	var backend microsub.Microsub

	if createBackend {
		backend = createMemoryBackend()
	} else {
		backend = loadMemoryBackend()
	}

	http.Handle("/microsub", &microsubHandler{backend})
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}
