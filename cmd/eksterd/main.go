/*
   Microsub server
   Copyright (C) 2018  Peter Stuifzand

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU General Public License for more details.

   You should have received a copy of the GNU General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gomodule/redigo/redis"
	"p83.nl/go/ekster/pkg/auth"

	"p83.nl/go/ekster/pkg/server"
)

const (
	ClientID string = "https://p83.nl/microsub-client"
)

type AppOptions struct {
	Port        int
	AuthEnabled bool
	Headless    bool
	RedisServer string
	BaseURL     string
	TemplateDir string
}

var (
	pool *redis.Pool
)

func init() {
	log.SetFlags(log.Lshortfile | log.Ldate | log.Ltime)
}

func newPool(addr string) *redis.Pool {
	return &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial:        func() (redis.Conn, error) { return redis.Dial("tcp", addr) },
	}
}

func WithAuth(handler http.Handler, b *memoryBackend) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			handler.ServeHTTP(w, r)
			return
		}

		authorization := r.Header.Get("Authorization")

		var token auth.TokenResponse

		if !b.AuthTokenAccepted(authorization, &token) {
			log.Printf("Token could not be validated")
			http.Error(w, "Can't validate token", 403)
			return
		}

		if token.Me != b.Me {
			log.Printf("Missing \"me\" in token response: %#v\n", token)
			http.Error(w, "Wrong me", 403)
			return
		}

		handler.ServeHTTP(w, r)
	})
}

type App struct {
	options    AppOptions
	backend    *memoryBackend
	hubBackend *hubIncomingBackend
}

func (app *App) Run() {
	app.backend.run()
	app.hubBackend.run()

	log.Printf("Listening on port %d\n", app.options.Port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", app.options.Port), nil))

}

func NewApp(options AppOptions) *App {
	app := &App{
		options: options,
	}

	app.backend = loadMemoryBackend()
	app.backend.AuthEnabled = options.AuthEnabled

	app.hubBackend = &hubIncomingBackend{app.backend, options.BaseURL}

	http.Handle("/micropub", &micropubHandler{
		Backend: app.backend,
	})

	handler := server.NewMicrosubHandler(app.backend)
	if options.AuthEnabled {
		handler = WithAuth(handler, app.backend)
	}

	http.Handle("/microsub", handler)

	http.Handle("/incoming/", &incomingHandler{
		Backend: app.hubBackend,
	})

	if !options.Headless {
		handler, err := newMainHandler(app.backend, options.BaseURL, options.TemplateDir)
		if err != nil {
			log.Fatal(err)
		}
		http.Handle("/", handler)
	}

	return app
}

func main() {
	log.Println("eksterd - microsub server")

	var options AppOptions

	flag.IntVar(&options.Port, "port", 80, "port for serving api")
	flag.BoolVar(&options.AuthEnabled, "auth", true, "use auth")
	flag.BoolVar(&options.Headless, "headless", false, "disable frontend")
	flag.StringVar(&options.RedisServer, "redis", "redis:6379", "redis server")
	flag.StringVar(&options.BaseURL, "baseurl", "", "http server baseurl")
	flag.StringVar(&options.TemplateDir, "templates", "./templates", "template directory")

	flag.Parse()

	if options.AuthEnabled {
		log.Println("Using auth")
	} else {
		log.Println("Authentication disabled")
	}

	if options.BaseURL == "" {
		if envVar, e := os.LookupEnv("EKSTER_BASEURL"); e {
			options.BaseURL = envVar
		} else {
			log.Fatal("EKSTER_BASEURL environment variable not found, please set with external url, -baseurl url option")
		}
	}

	if options.TemplateDir == "" {
		if envVar, e := os.LookupEnv("EKSTER_TEMPLATES"); e {
			options.TemplateDir = envVar
		} else {
			log.Fatal("EKSTER_TEMPLATES environment variable not found, use env var or -templates dir option")
		}
	}

	createBackend := false
	args := flag.Args()

	if len(args) >= 1 {
		if args[0] == "new" {
			createBackend = true
		}
	}

	if createBackend {
		createMemoryBackend()

		// TODO(peter): automatically gather this information from login or otherwise
		log.Println(`Config file "backend.json" is created in the current directory.`)
		log.Println(`Update "Me" variable to your website address "https://example.com/"`)
		log.Println(`Update "TokenEndpoint" variable to the address of your token endpoint "https://example.com/token"`)

		return
	}

	pool = newPool(options.RedisServer)

	NewApp(options).Run()
}
