package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublicURL_Methods(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		value      string
		wantSet    bool
		wantSecure bool
		wantHTTPS  bool
		wantOrigin string
	}{
		{
			name:       "given an https URL, then it is https, cookies are secure and the origin is the URL",
			value:      "https://verve.example.com",
			wantSet:    true,
			wantSecure: true,
			wantHTTPS:  true,
			wantOrigin: "https://verve.example.com",
		},
		{
			name:       "given an http URL with a port and a trailing slash, then it is not https and cookies are not secure",
			value:      "http://verve.example.com:8080/",
			wantSet:    true,
			wantOrigin: "http://verve.example.com:8080",
		},
		{
			name: "given no URL, then it is unset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var p PublicURL
			if tt.value != "" {
				require.NoError(t, p.UnmarshalText([]byte(tt.value)))
			}

			require.Equal(t, tt.wantSet, p.IsSet())
			require.Equal(t, tt.wantSecure, p.SecureCookies())
			require.Equal(t, tt.wantHTTPS, p.HTTPS())
			require.Equal(t, tt.wantOrigin, p.Origin())
			require.Equal(t, tt.wantOrigin, p.String())
		})
	}
}
