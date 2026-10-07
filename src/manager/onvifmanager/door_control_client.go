package onvifmanager

import (
	"bytes"
	"encoding/xml"
	"sort"
	"strings"
)

type doorInfo struct {
	Token     string
	Name      string
	LockToken string
}

func parseDoorInfoList(document []byte) ([]doorInfo, string) {
	decoder := xml.NewDecoder(bytes.NewReader(document))
	doors := make([]doorInfo, 0)
	nextReference := ""
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "DoorInfo":
			var raw struct {
				Name      string `xml:"Name"`
				LockToken string `xml:"LockToken"`
			}
			if err := decoder.DecodeElement(&raw, &start); err != nil {
				continue
			}
			door := doorInfo{Name: strings.TrimSpace(raw.Name), LockToken: strings.TrimSpace(raw.LockToken)}
			for _, attribute := range start.Attr {
				if attribute.Name.Local == "token" {
					door.Token = strings.TrimSpace(attribute.Value)
					break
				}
			}
			if door.Token != "" {
				doors = append(doors, door)
			}
		case "NextStartReference":
			_ = decoder.DecodeElement(&nextReference, &start)
			nextReference = strings.TrimSpace(nextReference)
		}
	}
	sort.Slice(doors, func(i, j int) bool { return doors[i].Token < doors[j].Token })
	return doors, nextReference
}

func doorCommandBody(command, token string) string {
	return `<tdc:` + command + `><tdc:Token>` + xmlEscape(token) + `</tdc:Token></tdc:` + command + `>`
}

func xmlEscape(value string) string {
	var buffer bytes.Buffer
	_ = xml.EscapeText(&buffer, []byte(value))
	return buffer.String()
}
