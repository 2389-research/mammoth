// ABOUTME: Server-Sent Events (SSE) streaming parser for the unified LLM client SDK.
// ABOUTME: Reads from an io.Reader and yields SSE events per the W3C EventSource specification.

package sse

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// Event represents a single Server-Sent Event parsed from a stream.
type Event struct {
	Type  string // from "event:" line, defaults to "message"
	Data  string // from "data:" line(s), joined with newlines for multi-line
	ID    string // from "id:" line
	Retry int    // from "retry:" line, -1 if not set
}

// Parser reads SSE events from an io.Reader.
type Parser struct {
	scanner *lineScanner
	done    bool

	// Accumulation state for the current event being built.
	eventType string
	dataLines []string
	hasData   bool
	id        string
	retry     int
}

// NewParser creates a new SSE parser that reads from the given reader.
func NewParser(reader io.Reader) *Parser {
	return &Parser{
		scanner: newLineScanner(reader),
		retry:   -1,
	}
}

// Next returns the next SSE event from the stream.
// Returns io.EOF when the stream ends.
func (p *Parser) Next() (Event, error) {
	if p.done {
		return Event{}, io.EOF
	}

	for {
		line, err := p.scanner.readLine()
		if err != nil {
			if err == io.EOF {
				// Stream ended. If we have pending data, dispatch it.
				if p.hasData {
					evt := p.buildEvent()
					p.resetState()
					p.done = true
					return evt, nil
				}
				p.done = true
				return Event{}, io.EOF
			}
			return Event{}, err
		}

		// A blank line dispatches the current event.
		if line == "" {
			if !p.hasData {
				// No data accumulated, skip (prevents empty events from consecutive blank lines).
				continue
			}
			evt := p.buildEvent()
			p.resetState()
			return evt, nil
		}

		// Comment lines start with ':'.
		if strings.HasPrefix(line, ":") {
			continue
		}

		// Parse field and value.
		field, value := parseLine(line)
		p.processField(field, value)
	}
}

// parseLine splits an SSE line into field name and value.
// If there is no colon, the entire line is the field name and value is empty.
// If there is a colon, the field is everything before the first colon,
// and the value is everything after, with a single leading space stripped.
func parseLine(line string) (field, value string) {
	colonIdx := strings.IndexByte(line, ':')
	if colonIdx == -1 {
		return line, ""
	}
	field = line[:colonIdx]
	value = line[colonIdx+1:]
	// Strip a single leading space if present.
	if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}
	return field, value
}

// processField handles a parsed SSE field.
func (p *Parser) processField(field, value string) {
	switch field {
	case "event":
		p.eventType = value
	case "data":
		if p.hasData {
			p.dataLines = append(p.dataLines, value)
		} else {
			p.dataLines = []string{value}
			p.hasData = true
		}
	case "id":
		p.id = value
	case "retry":
		n, err := strconv.Atoi(value)
		if err == nil {
			p.retry = n
		}
		// Invalid retry values are ignored per the SSE spec.
	default:
		// Unknown fields are ignored.
	}
}

// buildEvent constructs an Event from the current accumulated state.
func (p *Parser) buildEvent() Event {
	eventType := p.eventType
	if eventType == "" {
		eventType = "message"
	}
	return Event{
		Type:  eventType,
		Data:  strings.Join(p.dataLines, "\n"),
		ID:    p.id,
		Retry: p.retry,
	}
}

// resetState clears the accumulated event state for the next event.
func (p *Parser) resetState() {
	p.eventType = ""
	p.dataLines = nil
	p.hasData = false
	p.id = ""
	p.retry = -1
}

// lineScanner reads lines from an io.Reader, handling CR, LF, and CRLF line endings.
// bufio.Scanner only handles LF and CRLF natively, so we implement a custom scanner
// that also treats standalone CR as a line terminator.
type lineScanner struct {
	reader *bufio.Reader
}

func newLineScanner(r io.Reader) *lineScanner {
	return &lineScanner{reader: bufio.NewReaderSize(r, 4096)}
}

// readLine reads one line from the reader, stripping the line ending.
// Handles CR, LF, and CRLF as line terminators.
func (s *lineScanner) readLine() (string, error) {
	var line strings.Builder
	for {
		b, err := s.reader.ReadByte()
		if err != nil {
			if err == io.EOF {
				if line.Len() > 0 {
					return line.String(), nil
				}
				return "", io.EOF
			}
			return "", err
		}

		if b == '\n' {
			return line.String(), nil
		}

		if b == '\r' {
			// Check for CRLF. If next byte is LF, consume it.
			next, err := s.reader.ReadByte()
			if err == nil {
				if next != '\n' {
					// Not CRLF, just CR. Put the byte back.
					_ = s.reader.UnreadByte()
				}
			}
			// Either way, we've hit a line ending (CR or CRLF).
			return line.String(), nil
		}

		line.WriteByte(b)
	}
}
