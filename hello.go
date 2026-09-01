package main

import (
	"fmt"
	"net/http"
	"time"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello from unikraft, pid alive\n")
	})
	go func() {
		_ = http.ListenAndServe(":5244", nil)
	}()
	for {
		time.Sleep(time.Minute)
	}
}
