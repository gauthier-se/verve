package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Given the user agents of real browsers, then ParseUserAgent names their browser and device. Given
// a user agent it does not know, then it names what it recognizes, and nothing else.
func TestParseUserAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		userAgent string
		want      UserAgent
	}{
		{
			name:      "Safari on iPhone",
			userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1",
			want:      UserAgent{Browser: "Safari", Device: "iPhone"},
		},
		{
			name:      "Safari on iPad",
			userAgent: "Mozilla/5.0 (iPad; CPU OS 17_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.6 Mobile/15E148 Safari/604.1",
			want:      UserAgent{Browser: "Safari", Device: "iPad"},
		},
		{
			name:      "Safari on Mac",
			userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Safari/605.1.15",
			want:      UserAgent{Browser: "Safari", Device: "Mac"},
		},
		{
			name:      "Chrome on iPhone",
			userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/130.0.6723.90 Mobile/15E148 Safari/604.1",
			want:      UserAgent{Browser: "Chrome", Device: "iPhone"},
		},
		{
			name:      "Firefox on iPhone",
			userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/132.0 Mobile/15E148 Safari/605.1.15",
			want:      UserAgent{Browser: "Firefox", Device: "iPhone"},
		},
		{
			name:      "Chrome on Windows",
			userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
			want:      UserAgent{Browser: "Chrome", Device: "Windows"},
		},
		{
			name:      "Edge on Windows",
			userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/130.0.2849.68",
			want:      UserAgent{Browser: "Edge", Device: "Windows"},
		},
		{
			name:      "Firefox on Linux",
			userAgent: "Mozilla/5.0 (X11; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0",
			want:      UserAgent{Browser: "Firefox", Device: "Linux"},
		},
		{
			name:      "Firefox on Ubuntu",
			userAgent: "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0",
			want:      UserAgent{Browser: "Firefox", Device: "Linux"},
		},
		{
			name:      "Chrome on Android",
			userAgent: "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36",
			want:      UserAgent{Browser: "Chrome", Device: "Android"},
		},
		{
			name:      "Firefox on Android",
			userAgent: "Mozilla/5.0 (Android 15; Mobile; rv:132.0) Gecko/132.0 Firefox/132.0",
			want:      UserAgent{Browser: "Firefox", Device: "Android"},
		},
		{
			name:      "Samsung Internet on Android",
			userAgent: "Mozilla/5.0 (Linux; Android 14; SAMSUNG SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/26.0 Chrome/122.0.0.0 Mobile Safari/537.36",
			want:      UserAgent{Browser: "Samsung Internet", Device: "Android"},
		},
		{
			name:      "Opera on Mac",
			userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36 OPR/114.0.0.0",
			want:      UserAgent{Browser: "Opera", Device: "Mac"},
		},
		{
			name:      "Chrome on ChromeOS",
			userAgent: "Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
			want:      UserAgent{Browser: "Chrome", Device: "ChromeOS"},
		},
		{name: "browser alone", userAgent: "Mozilla/5.0 Firefox/132.0", want: UserAgent{Browser: "Firefox"}},
		{name: "device alone", userAgent: "Mozilla/5.0 (iPhone)", want: UserAgent{Device: "iPhone"}},
		{name: "command line client", userAgent: "curl/8.11.0", want: UserAgent{}},
		{name: "empty", userAgent: "", want: UserAgent{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, ParseUserAgent(tt.userAgent))
		})
	}
}
