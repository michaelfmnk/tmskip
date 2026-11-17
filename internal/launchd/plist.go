// Package launchd renders, parses and loads tmskip's LaunchAgent.
package launchd

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Label is the LaunchAgent label.
const Label = "dev.tmskip.agent"

// PlistPath is where the agent's plist lives.
func PlistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist")
}

// Agent is what tmskip puts in the plist.
type Agent struct {
	Binary   string // stable path the user invoked, never the Cellar target
	Config   string // absolute config path, symlink kept
	Interval time.Duration
	LogPath  string // agent stdout/stderr
}

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// Render returns the plist XML.
func Render(a Agent) ([]byte, error) {
	if a.Binary == "" || a.Config == "" || a.Interval <= 0 {
		return nil, errors.New("agent needs a binary, a config and an interval")
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + Label + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + esc(a.Binary) + `</string>
		<string>sync</string>
		<string>-q</string>
		<string>--config</string>
		<string>` + esc(a.Config) + `</string>
	</array>
	<key>StartInterval</key>
	<integer>` + strconv.FormatInt(int64(a.Interval/time.Second), 10) + `</integer>
	<key>RunAtLoad</key>
	<false/>
	<key>ProcessType</key>
	<string>Background</string>
	<key>LowPriorityIO</key>
	<true/>
	<key>EnvironmentVariables</key>
	<dict>
		<key>TMSKIP_AGENT</key>
		<string>1</string>
	</dict>
`)
	if a.LogPath != "" {
		b.WriteString("\t<key>StandardOutPath</key>\n\t<string>" + esc(a.LogPath) + "</string>\n")
		b.WriteString("\t<key>StandardErrorPath</key>\n\t<string>" + esc(a.LogPath) + "</string>\n")
	}
	b.WriteString("</dict>\n</plist>\n")
	return []byte(b.String()), nil
}

// Parse reads back an agent plist (XML format, as Render writes it).
func Parse(b []byte) (Agent, error) {
	top, err := decodePlist(b)
	if err != nil {
		return Agent{}, err
	}
	dict, ok := top.(map[string]any)
	if !ok {
		return Agent{}, errors.New("agent plist: top level is not a dict")
	}
	var a Agent
	args, _ := dict["ProgramArguments"].([]any)
	if len(args) == 0 {
		return Agent{}, errors.New("agent plist: no ProgramArguments")
	}
	a.Binary, _ = args[0].(string)
	for i := 1; i+1 < len(args); i++ {
		if args[i] == "--config" {
			a.Config, _ = args[i+1].(string)
		}
	}
	if n, ok := dict["StartInterval"].(int64); ok {
		a.Interval = time.Duration(n) * time.Second
	}
	a.LogPath, _ = dict["StandardOutPath"].(string)
	return a, nil
}

// ProgramArguments returns the plist's ProgramArguments strings.
func ProgramArguments(b []byte) ([]string, error) {
	top, err := decodePlist(b)
	if err != nil {
		return nil, err
	}
	dict, _ := top.(map[string]any)
	raw, _ := dict["ProgramArguments"].([]any)
	var out []string
	for _, v := range raw {
		s, _ := v.(string)
		out = append(out, s)
	}
	return out, nil
}

// decodePlist decodes an XML plist into Go values: map[string]any, []any,
// string, int64, bool (other types are kept as their text).
func decodePlist(b []byte) (any, error) {
	d := xml.NewDecoder(bytes.NewReader(b))
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, fmt.Errorf("agent plist: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Local != "plist" {
				return nil, errors.New("agent plist: not a plist")
			}
			for {
				tok, err := d.Token()
				if err != nil {
					return nil, fmt.Errorf("agent plist: %w", err)
				}
				if se, ok := tok.(xml.StartElement); ok {
					return decodeValue(d, se)
				}
			}
		}
	}
}

func decodeValue(d *xml.Decoder, se xml.StartElement) (any, error) {
	switch se.Name.Local {
	case "dict":
		m := map[string]any{}
		key := ""
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				if t.Name.Local == "key" {
					s, err := text(d)
					if err != nil {
						return nil, err
					}
					key = s
					continue
				}
				v, err := decodeValue(d, t)
				if err != nil {
					return nil, err
				}
				m[key] = v
			case xml.EndElement:
				return m, nil
			}
		}
	case "array":
		var arr []any
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				v, err := decodeValue(d, t)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			case xml.EndElement:
				return arr, nil
			}
		}
	case "true", "false":
		if err := d.Skip(); err != nil {
			return nil, err
		}
		return se.Name.Local == "true", nil
	case "integer":
		s, err := text(d)
		if err != nil {
			return nil, err
		}
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("agent plist: bad integer %q", s)
		}
		return n, nil
	default: // string, real, date, data: their text
		return text(d)
	}
}

// text reads character data up to the current element's end.
func text(d *xml.Decoder) (string, error) {
	var b strings.Builder
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return "", io.ErrUnexpectedEOF
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.EndElement:
			return b.String(), nil
		}
	}
}
