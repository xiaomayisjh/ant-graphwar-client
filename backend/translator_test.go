package backend

import (
	"strings"
	"testing"
)

// Network test (run without -short): the YouDao translate proxy works both ways.
func TestTranslate(t *testing.T) {
	if testing.Short() {
		t.Skip("network test")
	}
	tr := NewTranslator()
	zh, err := tr.Translate("hello world, nice to meet you", "zh-CHS")
	if err != nil {
		t.Fatalf("en->zh: %v", err)
	}
	t.Logf("en->zh: %q", zh)
	if zh == "" {
		t.Fatal("empty zh translation")
	}
	en, err := tr.Translate("国人好呀", "en")
	if err != nil {
		t.Fatalf("zh->en: %v", err)
	}
	t.Logf("zh->en: %q", en)
	if !strings.ContainsAny(en, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatal("zh->en produced no latin text")
	}
}
