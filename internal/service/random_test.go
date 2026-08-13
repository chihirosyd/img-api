package service

import (
	"reflect"
	"testing"
)

func TestSplitCategories(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{"", []string{"default"}},
		{"anime", []string{"anime"}},
		{"anime, scenery , anime", []string{"anime", "scenery"}}, // 去重 + 去空格
		{" , , ", []string{"default"}},
		{"a,b,c", []string{"a", "b", "c"}},
	}

	for _, c := range cases {
		got := splitCategories(c.raw)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitCategories(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}
