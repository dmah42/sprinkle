// Package main defines a UI for visualizing flocks.
package main

import (
	"flag"
	"fmt"
	"html"
	"html/template"
	"log"
	"net/http"
	"time"
)

var (
	port = flag.Int("port", 0, "The port on which to listen")
)

func handleError(w http.ResponseWriter, code int, err error) {
	w.WriteHeader(code)
	fmt.Fprintf(w, "%q", html.EscapeString(err.Error()))
	fmt.Printf("[E] %q\n", err)
}

func Index(w http.ResponseWriter, req *http.Request) {
	t, err := template.New("index").Parse(
		`<html><body>
		</body></html>`)
	if err != nil {
		handleError(w, http.StatusInternalServerError, err)
		return
	}

	if err = t.Execute(w, nil); err != nil {
		handleError(w, http.StatusInternalServerError, err)
		return
	}
}

func main() {
	flag.Parse()

	go func() {
		time.Sleep(5 * time.Minute)

		// TODO: discovery scan
	}()

	http.HandleFunc("/", Index)
	fmt.Printf("listening on port %d\n", *port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}
