package onvifmanager

import (
	"bytes"
	"encoding/xml"
	"strings"
)

func FindFirstText(document []byte, localName string) string {
	decoder := xml.NewDecoder(bytes.NewReader(document))
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != localName {
			continue
		}
		var text string
		if err := decoder.DecodeElement(&text, &start); err != nil {
			return ""
		}
		return strings.TrimSpace(text)
	}
}

func FindAllText(document []byte, localName string) []string {
	decoder := xml.NewDecoder(bytes.NewReader(document))
	result := make([]string, 0)
	for {
		token, err := decoder.Token()
		if err != nil {
			return result
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != localName {
			continue
		}
		var text string
		if err := decoder.DecodeElement(&text, &start); err != nil {
			return result
		}
		result = append(result, strings.TrimSpace(text))
	}
}
