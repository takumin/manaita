package deploy

import (
	"reflect"
	"testing"
)

func TestSudoArgs(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{
			name: "no proxy",
			want: []string{"sudo"},
		},
		{
			name: "set proxies only",
			env:  map[string]string{"https_proxy": "http://proxy:3128", "NO_PROXY": "localhost", "http_proxy": ""},
			want: []string{"sudo", "--preserve-env=https_proxy,NO_PROXY"},
		},
		{
			name: "all proxies",
			env: map[string]string{
				"http_proxy": "p", "https_proxy": "p", "ftp_proxy": "p", "all_proxy": "p", "no_proxy": "p",
				"HTTP_PROXY": "p", "HTTPS_PROXY": "p", "FTP_PROXY": "p", "ALL_PROXY": "p", "NO_PROXY": "p",
			},
			want: []string{"sudo", "--preserve-env=http_proxy,https_proxy,ftp_proxy,all_proxy,no_proxy,HTTP_PROXY,HTTPS_PROXY,FTP_PROXY,ALL_PROXY,NO_PROXY"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, name := range proxyVars {
				t.Setenv(name, tt.env[name])
			}
			if got := sudoArgs(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("want %q, got %q", tt.want, got)
			}
		})
	}
}
