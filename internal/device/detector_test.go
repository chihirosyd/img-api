package device

import (
	"testing"

	"img-api/internal/model"
)

func TestDetect(t *testing.T) {
	cases := []struct {
		ua   string
		want model.DeviceType
	}{
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36", model.DevicePC},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)", model.DevicePE},
		{"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36", model.DevicePE},
		{"Mozilla/5.0 (iPad; CPU OS 16_0 like Mac OS X)", model.DevicePE},
		{"", model.DevicePC},
		{"curl/8.0", model.DevicePC},
	}

	for _, c := range cases {
		if got := Detect(c.ua); got != c.want {
			t.Errorf("Detect(%q) = %s, want %s", c.ua, got, c.want)
		}
	}
}

func TestResolve(t *testing.T) {
	// 显式指定优先于 UA 检测
	if got := Resolve(model.DevicePE, "desktop browser"); got != model.DevicePE {
		t.Errorf("Resolve(explicit pe) = %s, want pe", got)
	}
	if got := Resolve(model.DevicePC, "mobile phone"); got != model.DevicePC {
		t.Errorf("Resolve(explicit pc) = %s, want pc", got)
	}
	// auto 走 UA 检测
	if got := Resolve(model.DeviceAuto, "desktop browser"); got != model.DevicePC {
		t.Errorf("Resolve(auto desktop) = %s, want pc", got)
	}
}
