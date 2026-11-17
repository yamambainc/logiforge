package ports

import "testing"

func TestCheckDirectories(t *testing.T) {
	ok := []Repository{{Name: "a", GitForgeRef: "acme/app:logiforge/a"}, {Name: "b", GitForgeRef: "acme/app:logiforge/b"}, {Name: "c", GitForgeRef: "acme/other"}}
	if err := CheckDirectories(ok); err != nil {
		t.Fatal(err)
	}
	for name, rs := range map[string][]Repository{
		"名前の重なり":     {{Name: "a", GitForgeRef: "acme/app:x"}, {Name: "a", GitForgeRef: "acme/app:y"}},
		"ディレクトリの重なり": {{Name: "a", GitForgeRef: "acme/app:logiforge"}, {Name: "b", GitForgeRef: "acme/app:logiforge/b"}},
		"大文字と小文字":    {{Name: "a", GitForgeRef: "acme/app:x"}, {Name: "b", GitForgeRef: "Acme/App:x"}},
		"全体とディレクトリ":  {{Name: "a", GitForgeRef: "acme/app"}, {Name: "b", GitForgeRef: "ACME/app:logiforge/b"}},
		"正しくないパス":    {{Name: "a", GitForgeRef: "acme/app:logiforge/../x"}},
	} {
		if err := CheckDirectories(rs); err == nil {
			t.Errorf("%s を通した", name)
		}
	}
}
