// Command status serves a fixed status document over HTTP on port 8443. It
// keeps no state and reaches no other service.
package main

import (
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	log.Fatal(http.ListenAndServe(":8443", nil))
}
