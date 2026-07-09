package object

import (
	"bytes"
	"fmt"
	"strings"
)

const tagSignatureHeader = "signature "

// TagSigningPayload returns the canonical annotated tag bytes that are signed.
// The payload is the tag data with any "signature " header removed.
func TagSigningPayload(data []byte) []byte {
	payload, _ := tagSigningPayloadAndSignature(data)
	return payload
}

// TagSignature returns the native Graft signature header value from annotated
// tag data, or an empty string when the tag is unsigned.
func TagSignature(data []byte) string {
	_, signature := tagSigningPayloadAndSignature(data)
	return signature
}

// AddTagSignature inserts signature as a tag header and removes any existing
// native signature header first.
func AddTagSignature(data []byte, signature string) []byte {
	payload := TagSigningPayload(data)
	idx := bytes.Index(payload, []byte("\n\n"))
	if idx < 0 {
		out := append([]byte{}, payload...)
		if len(out) > 0 && out[len(out)-1] != '\n' {
			out = append(out, '\n')
		}
		return append(out, []byte(fmt.Sprintf("%s%s\n", tagSignatureHeader, strings.TrimSpace(signature)))...)
	}

	header := payload[:idx]
	body := payload[idx+2:]
	var out bytes.Buffer
	out.Write(header)
	if len(header) > 0 && header[len(header)-1] != '\n' {
		out.WriteByte('\n')
	}
	fmt.Fprintf(&out, "%s%s\n\n", tagSignatureHeader, strings.TrimSpace(signature))
	out.Write(body)
	return out.Bytes()
}

func tagSigningPayloadAndSignature(data []byte) ([]byte, string) {
	idx := bytes.Index(data, []byte("\n\n"))
	if idx < 0 {
		out := append([]byte{}, data...)
		return out, ""
	}

	header := data[:idx]
	body := data[idx+2:]
	lines := bytes.Split(header, []byte("\n"))
	filtered := make([][]byte, 0, len(lines))
	signature := ""
	for _, line := range lines {
		if bytes.HasPrefix(line, []byte(tagSignatureHeader)) {
			if signature == "" {
				signature = strings.TrimSpace(string(line[len(tagSignatureHeader):]))
			}
			continue
		}
		filtered = append(filtered, line)
	}

	var out bytes.Buffer
	for i, line := range filtered {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.Write(line)
	}
	out.WriteString("\n\n")
	out.Write(body)
	return out.Bytes(), signature
}
