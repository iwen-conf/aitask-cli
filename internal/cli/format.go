package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

type RenderData struct {
	Brief  string
	Prompt string
	JSON   any
	Proto  proto.Message
}

type Printer struct {
	format OutputFormat
	w      io.Writer
}

func NewPrinter(format OutputFormat, w io.Writer) *Printer {
	return &Printer{format: format, w: w}
}

func (p *Printer) Print(data RenderData) error {
	switch p.format {
	case FormatBrief:
		return writeText(p.w, pickText(data.Brief, data.Prompt))
	case FormatPrompt:
		return writeText(p.w, pickText(data.Prompt, data.Brief))
	case FormatJSON:
		payload, err := marshalRenderJSON(data)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(p.w, string(payload))
		return err
	case FormatProto:
		payload, err := marshalRenderProto(data)
		if err != nil {
			return err
		}
		_, err = p.w.Write(payload)
		if err != nil {
			return err
		}
		if !endsWithNewline(payload) {
			_, err = fmt.Fprintln(p.w)
		}
		return err
	default:
		return fmt.Errorf("unsupported format: %s", p.format)
	}
}

func pickText(primary string, fallback string) string {
	text := strings.TrimSpace(primary)
	if text == "" {
		text = strings.TrimSpace(fallback)
	}
	return text
}

func writeText(w io.Writer, text string) error {
	if strings.TrimSpace(text) == "" {
		text = "(empty)"
	}
	_, err := fmt.Fprintln(w, text)
	return err
}

func marshalRenderJSON(data RenderData) ([]byte, error) {
	if data.JSON != nil {
		return json.MarshalIndent(data.JSON, "", "  ")
	}
	if data.Proto != nil {
		payload, err := protojson.MarshalOptions{Indent: "  "}.Marshal(data.Proto)
		if err != nil {
			return nil, err
		}
		var out any
		if err := json.Unmarshal(payload, &out); err != nil {
			return nil, err
		}
		return json.MarshalIndent(out, "", "  ")
	}
	return json.MarshalIndent(map[string]any{}, "", "  ")
}

func marshalRenderProto(data RenderData) ([]byte, error) {
	if data.Proto != nil {
		return proto.Marshal(data.Proto)
	}
	if data.JSON == nil {
		msg := &structpb.Struct{Fields: map[string]*structpb.Value{}}
		return proto.Marshal(msg)
	}
	raw, err := json.Marshal(data.JSON)
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	msg, err := structpb.NewStruct(obj)
	if err != nil {
		return nil, err
	}
	return proto.Marshal(msg)
}

func endsWithNewline(payload []byte) bool {
	if len(payload) == 0 {
		return false
	}
	return payload[len(payload)-1] == '\n'
}
