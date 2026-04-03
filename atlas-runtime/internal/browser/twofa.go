package browser

import (
	"fmt"
	"strings"

	"github.com/go-rod/rod"
)

// TwoFAResult is the output of Detect2FA.
type TwoFAResult struct {
	Detected      bool
	InputSelector string // CSS selector for the code input
	Prompt        string // human-readable prompt for the user
}

// Detect2FA checks whether the current page is a 2FA challenge screen.
// It applies URL, title, and DOM heuristics.
//
// All DOM checks use page.Has (non-blocking) — the function returns immediately
// based on what is currently rendered and never retries.
func Detect2FA(page *rod.Page) *TwoFAResult {
	result := &TwoFAResult{}

	info, err := page.Info()
	if err != nil {
		return result
	}

	urlLower := strings.ToLower(info.URL)
	titleLower := strings.ToLower(info.Title)

	// ── Heuristic 1: URL patterns ─────────────────────────────────────────────
	twoFAPaths := []string{
		"/two-factor", "/2fa", "/otp", "/verify", "/totp",
		"/mfa", "/multi-factor", "/auth/otp", "/auth/totp",
		"/security/two-factor", "/challenge",
	}
	for _, p := range twoFAPaths {
		if strings.Contains(urlLower, p) {
			result.Detected = true
			break
		}
	}

	// ── Heuristic 2: Page title ───────────────────────────────────────────────
	if !result.Detected {
		twoFATitles := []string{
			"two-factor", "2fa", "verification code", "authenticator",
			"one-time", "otp", "second factor", "multi-factor",
		}
		for _, kw := range twoFATitles {
			if strings.Contains(titleLower, kw) {
				result.Detected = true
				break
			}
		}
	}

	// ── Heuristic 3: DOM — OTP input field ───────────────────────────────────
	// page.Has is non-blocking: returns immediately based on current DOM state.
	otpSelectors := []string{
		`input[autocomplete="one-time-code"]`,
		`input[name="otp"]`,
		`input[name="totp"]`,
		`input[name="token"]`,
		`input[name="code"]`,
		`input[name="mfa_code"]`,
		`input[name="two_factor_code"]`,
		`input[inputmode="numeric"][maxlength="6"]`,
	}
	for _, sel := range otpSelectors {
		if has, _, err := page.Has(sel); err == nil && has {
			result.Detected = true
			result.InputSelector = sel
			break
		}
	}

	if result.Detected {
		// Compose a human-readable prompt.
		site := extractHost(info.URL)
		if result.InputSelector != "" {
			result.Prompt = fmt.Sprintf(
				"%s requires a 2FA code. Call vault.totp_generate if a TOTP secret is stored, "+
					"or ask the user to provide the code. Then call browser.session_submit_2fa with the code.",
				site)
		} else {
			result.Prompt = fmt.Sprintf(
				"%s appears to require a 2FA code but the input field could not be identified automatically. "+
					"Inspect the page with browser.read_page and use browser.fill_form to submit the code manually.",
				site)
		}
	}
	return result
}
