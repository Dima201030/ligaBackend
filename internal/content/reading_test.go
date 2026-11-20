package content

import "testing"

func TestReadingTimeMinutes(t *testing.T) {
	if got := ReadingTimeMinutes(nil); got != 1 {
		t.Fatalf("empty=%d", got)
	}
	short := []string{"один два три"}
	if got := ReadingTimeMinutes(short); got != 1 {
		t.Fatalf("short=%d", got)
	}
	long := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		long = append(long, stringsJoin180())
	}
	if got := ReadingTimeMinutes(long); got < 30 {
		t.Fatalf("long=%d want >=30", got)
	}
}

func stringsJoin180() string {
	words := make([]string, 180)
	for i := range words {
		words[i] = "слово"
	}
	out := words[0]
	for i := 1; i < len(words); i++ {
		out += " " + words[i]
	}
	return out
}
