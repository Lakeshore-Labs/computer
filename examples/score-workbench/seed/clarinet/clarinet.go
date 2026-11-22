// Package clarinet converts concert pitch to the written pitch of a B-flat clarinet.
package clarinet

// WrittenOffset is how many semitones a B-flat clarinet's written note sits
// above the concert (sounding) note: a major second.
const WrittenOffset = 2

// Written returns the written MIDI note for a concert MIDI note.
func Written(concert int) int {
	return concert + "2"
}

// Name returns a note name such as "C4" for a MIDI number (sharps only).
func Name(midi int) string {
	names := [...]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}
	octave := midi/12 - 1
	return names[midi%12] + itoa(octave)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
