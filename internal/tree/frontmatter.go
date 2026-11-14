package tree

import (
	"bytes"
	"errors"
	"fmt"

	"go.yaml.in/yaml/v3"
)

// splitFrontMatter は、YAML の front matter と Markdown の本文を分ける（design/files/layout.md の「ファイルの中身」）。
// front matter がなければ、data は nil である。
func splitFrontMatter(content []byte) (data map[string]any, body string, err error) {
	if !bytes.HasPrefix(content, []byte("---\n")) {
		return nil, string(content), nil
	}
	rest := content[4:]
	end := bytes.Index(rest, []byte("\n---\n"))
	var yamlPart, bodyPart []byte
	switch {
	case end >= 0:
		yamlPart, bodyPart = rest[:end+1], rest[end+5:]
	case bytes.HasSuffix(rest, []byte("\n---")):
		yamlPart, bodyPart = rest[:len(rest)-3], nil
	default:
		return nil, "", errors.New("front matter が閉じていない")
	}
	var v any
	if err := yaml.Unmarshal(yamlPart, &v); err != nil {
		return nil, "", fmt.Errorf("front matter が YAML として読めない: %w", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, "", errors.New("front matter がオブジェクトでない")
	}
	return m, string(bodyPart), nil
}

// joinFrontMatter は、front matter と本文をつなぐ。keys の順に書く。
func joinFrontMatter(keys []string, data map[string]any, body string) []byte {
	var node yaml.Node
	node.Kind = yaml.MappingNode
	for _, k := range keys {
		v, ok := data[k]
		if !ok {
			continue
		}
		var kv, vv yaml.Node
		kv.SetString(k)
		if err := vv.Encode(v); err != nil {
			panic(err)
		}
		node.Content = append(node.Content, &kv, &vv)
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&node); err != nil {
		panic(err)
	}
	_ = enc.Close()
	buf.WriteString("---\n")
	buf.WriteString(body)
	return buf.Bytes()
}
