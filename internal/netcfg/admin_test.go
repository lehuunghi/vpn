package netcfg

import "testing"

func TestLANAdminPort(t *testing.T) {
	for _, tc := range []struct {
		address string
		want    int
	}{
		{"", 0}, {"127.0.0.1:51821", 0}, {"127.8.0.1:1234", 0},
		{"[::1]:51821", 0}, {"localhost:51821", 0},
		{":51821", 51821}, {"0.0.0.0:51822", 51822},
		{"192.168.1.100:8080", 8080}, {"[::]:51821", 51821},
	} {
		got, err := lanAdminPort(tc.address)
		if err != nil || got != tc.want {
			t.Errorf("%s: %d, %v; want %d", tc.address, got, err, tc.want)
		}
	}
	if _, err := lanAdminPort("0.0.0.0:99999"); err == nil {
		t.Fatal("invalid port accepted")
	}
}
