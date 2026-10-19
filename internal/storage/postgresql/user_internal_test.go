package postgresql

import "testing"

// Внутренний тест: likeEscaper не экспортируется. Как экранированный запрос
// работает в настоящей базе, проверяют интеграционные тесты поиска.
func TestLikeEscaper(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{in: "Alice", want: "Alice"},
		{in: "100%", want: `100\%`},
		{in: "a_b", want: `a\_b`},
		{in: `back\slash`, want: `back\\slash`},
		// Обратную косую черту тоже нужно экранировать: иначе из \% получилось бы \\%,
		// где \\ - буквальная черта, а % снова работает как шаблон.
		{in: `\%`, want: `\\\%`},
		{in: "%_%", want: `\%\_\%`},
		{in: "", want: ""},
	}

	for _, tt := range tests {
		if got := likeEscaper.Replace(tt.in); got != tt.want {
			t.Errorf("likeEscaper.Replace(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
