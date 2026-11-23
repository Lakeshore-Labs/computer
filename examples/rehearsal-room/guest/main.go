// The WASI command reads a concert-pitch CSV score and writes a performer's part.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
)

type note struct {
	measure, pitch, beats int
}

type part struct {
	name                           string
	writtenOffset, fifths, program int
}

var parts = map[string]part{
	"flute":    {"Flute", 0, 0, 73},
	"clarinet": {"B-flat clarinet", 2, 2, 71},
}

func score(input io.Reader) ([]note, error) {
	reader := csv.NewReader(input)
	header, err := reader.Read()
	if err != nil || len(header) != 3 || header[0] != "measure" || header[1] != "concert_midi" || header[2] != "beats" {
		return nil, errors.New("expected measure,concert_midi,beats")
	}
	var result []note
	measure, beat := 1, 0
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		m, mErr := strconv.Atoi(row[0])
		p, pErr := strconv.Atoi(row[1])
		b, bErr := strconv.Atoi(row[2])
		if mErr != nil || pErr != nil || bErr != nil || m != measure || p < 48 || p > 84 || (b != 1 && b != 2 && b != 4) || beat+b > 4 {
			return nil, fmt.Errorf("invalid note on row %d", len(result)+2)
		}
		result = append(result, note{m, p, b})
		beat += b
		if beat == 4 {
			measure++
			beat = 0
		}
	}
	if len(result) == 0 || beat != 0 {
		return nil, errors.New("each measure must have four beats")
	}
	return result, nil
}

func musicXML(notes []note, instrument part) []byte {
	var out bytes.Buffer
	fmt.Fprintf(&out, `<?xml version="1.0" encoding="UTF-8"?>
<score-partwise version="4.0"><work><work-title>Last Boat Home</work-title></work>
<part-list><score-part id="P1"><part-name>%s</part-name>
<score-instrument id="P1-I1"><instrument-name>%s</instrument-name></score-instrument>
<midi-instrument id="P1-I1"><midi-channel>1</midi-channel><midi-program>%d</midi-program></midi-instrument>
</score-part></part-list><part id="P1">
`, instrument.name, instrument.name, instrument.program+1)
	steps := [...]string{"C", "C", "D", "D", "E", "F", "F", "G", "G", "A", "A", "B"}
	sharp := [...]bool{false, true, false, true, false, false, true, false, true, false, true, false}
	duration := map[int]string{1: "quarter", 2: "half", 4: "whole"}
	measure := 0
	for _, n := range notes {
		if n.measure != measure {
			if measure > 0 {
				out.WriteString("</measure>\n")
			}
			measure = n.measure
			fmt.Fprintf(&out, "<measure number=\"%d\">\n", measure)
			if measure == 1 {
				fmt.Fprintf(&out, "<attributes><divisions>1</divisions><key><fifths>%d</fifths></key><time><beats>4</beats><beat-type>4</beat-type></time><clef><sign>G</sign><line>2</line></clef>", instrument.fifths)
				if instrument.writtenOffset != 0 {
					out.WriteString("<transpose><diatonic>-1</diatonic><chromatic>-2</chromatic></transpose>")
				}
				out.WriteString("</attributes>\n<direction><direction-type><metronome><beat-unit>quarter</beat-unit><per-minute>96</per-minute></metronome></direction-type><sound tempo=\"96\"/></direction>\n")
			}
		}
		written := n.pitch + instrument.writtenOffset
		fmt.Fprintf(&out, "<note><pitch><step>%s</step>", steps[written%12])
		if sharp[written%12] {
			out.WriteString("<alter>1</alter>")
		}
		fmt.Fprintf(&out, "<octave>%d</octave></pitch><duration>%d</duration><type>%s</type></note>\n", written/12-1, n.beats, duration[n.beats])
	}
	out.WriteString("</measure></part></score-partwise>\n")
	return out.Bytes()
}

func midi(notes []note, instrument part) []byte {
	const ticks = 480
	var track bytes.Buffer
	// A quarter note takes 625,000 microseconds at 96 beats per minute.
	track.Write([]byte{0, 0xff, 0x51, 3, 0x09, 0x89, 0x68, 0, 0xff, 0x58, 4, 4, 2, 24, 8, 0, 0xc0, byte(instrument.program)})
	for _, n := range notes {
		track.Write([]byte{0, 0x90, byte(n.pitch), 80})
		delta := n.beats * ticks
		// The supported note lengths occupy exactly two variable-length bytes.
		track.Write([]byte{byte(delta>>7) | 0x80, byte(delta & 0x7f), 0x80, byte(n.pitch), 0})
	}
	track.Write([]byte{0, 0xff, 0x2f, 0})
	var out bytes.Buffer
	out.Write([]byte{'M', 'T', 'h', 'd', 0, 0, 0, 6, 0, 0, 0, 1, 0x01, 0xe0, 'M', 'T', 'r', 'k'})
	out.Write(binary.BigEndian.AppendUint32(nil, uint32(track.Len())))
	out.Write(track.Bytes())
	return out.Bytes()
}

func run() error {
	if len(os.Args) != 3 {
		return errors.New("usage: part flute|clarinet musicxml|midi < score.csv")
	}
	chosen, ok := parts[os.Args[1]]
	if !ok {
		return errors.New("unknown instrument")
	}
	notes, err := score(os.Stdin)
	if err != nil {
		return err
	}
	var data []byte
	switch os.Args[2] {
	case "musicxml":
		data = musicXML(notes, chosen)
	case "midi":
		data = midi(notes, chosen)
	default:
		return errors.New("unknown format")
	}
	_, err = os.Stdout.Write(data)
	return err
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
