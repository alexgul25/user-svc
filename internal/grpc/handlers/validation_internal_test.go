package handlersgrpc

import (
	"strings"
	"testing"
)

// Внутренний тест: normalizeUUID не экспортируется. Тесты хэндлеров проверяют,
// какой код получает клиент, а здесь перебираются варианты строки, которые
// регулярное выражение должно принять или отклонить. Через gRPC каждый такой
// вариант стоил бы отдельного запуска сервера.
func TestNormalizeUUID(t *testing.T) {
	const canonical = "4f1c2d3e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"

	valid := []struct {
		name string
		in   string
		want string
	}{
		{name: "canonical lower case", in: canonical, want: canonical},
		{name: "upper case", in: strings.ToUpper(canonical), want: canonical},
		{name: "mixed case", in: "4F1c2D3e-5A6b-4C7d-8E9f-0A1b2C3d4E5f", want: canonical},
		{name: "nil UUID", in: "00000000-0000-0000-0000-000000000000", want: "00000000-0000-0000-0000-000000000000"},
		{name: "max UUID", in: "FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF", want: "ffffffff-ffff-ffff-ffff-ffffffffffff"},
	}

	for _, tt := range valid {
		t.Run("valid/"+tt.name, func(t *testing.T) {
			got, ok := normalizeUUID(tt.in)
			if !ok || got != tt.want {
				t.Errorf("normalizeUUID(%q) = (%q, %v), want (%q, true)", tt.in, got, ok, tt.want)
			}
		})
	}

	invalid := []struct {
		name string
		in   string
	}{
		{name: "empty", in: ""},
		{name: "prefix before UUID", in: "x" + canonical},
		{name: "suffix after UUID", in: canonical + "x"},
		{name: "leading space", in: " " + canonical},
		{name: "trailing newline", in: canonical + "\n"},
		{name: "braces", in: "{" + canonical + "}"},
		{name: "URN prefix", in: "urn:uuid:" + canonical},
		{name: "no hyphens", in: strings.ReplaceAll(canonical, "-", "")},
		{name: "one hyphen missing", in: strings.Replace(canonical, "-", "", 1)},
		{name: "hyphen in wrong position", in: "4f1c2d3-e5a6b-4c7d-8e9f-0a1b2c3d4e5f"},
		{name: "non-hex letter in first group", in: "gf1c2d3e-5a6b-4c7d-8e9f-0a1b2c3d4e5f"},
		{name: "non-hex letter in last group", in: "4f1c2d3e-5a6b-4c7d-8e9f-0a1b2c3d4e5z"},
		// Кириллическая «е» выглядит как латинская e, но это другой символ.
		{name: "cyrillic look-alike letter", in: "4f1c2d3е-5a6b-4c7d-8e9f-0a1b2c3d4e5f"},
		{name: "extra group", in: canonical + "-0000"},
	}

	for _, tt := range invalid {
		t.Run("invalid/"+tt.name, func(t *testing.T) {
			if got, ok := normalizeUUID(tt.in); ok {
				t.Errorf("normalizeUUID(%q) = (%q, true), want rejection", tt.in, got)
			}
		})
	}
}
