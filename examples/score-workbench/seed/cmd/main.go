package main

import (
	"fmt"

	"example.test/scoredesk/clarinet"
)

func main() {
	concert := 60
	w := clarinet.Written(concert)
	fmt.Printf("concert %s (MIDI %d) -> written %s (MIDI %d)\n",
		clarinet.Name(concert), concert, clarinet.Name(w), w)
}
