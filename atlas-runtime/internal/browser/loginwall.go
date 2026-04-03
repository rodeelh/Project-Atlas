package browser

import (
	"strings"

	"github.com/go-rod/rod"
)

// LoginWallResult is the output of DetectLoginWall.
type LoginWallResult struct {
	Detected              bool
	UsernameInputSelector string // CSS selector, may be empty
	PasswordInputSelector string // CSS selector, may be empty
	FormSelector          string // CSS selector of the enclosing form, may be empty
}

// DetectLoginWall applies a layered set of heuristics to decide whether the
// current page is a login wall. The heuristics run cheapest-first.
//
// All DOM checks use page.Has (non-blocking) — the function returns immediately
// based on what is currently rendered and never retries.
func DetectLoginWall(page *rod.Page) *LoginWallResult {
	result := &LoginWallResult{}

	info, err := page.Info()
	if err != nil {
		return result
	}

	// ── Heuristic 1: URL path patterns ───────────────────────────────────────
	urlLower := strings.ToLower(info.URL)
	loginPaths := []string{
		"/login", "/signin", "/sign-in", "/auth/", "/accounts/login",
		"/session/new", "/user/login", "/users/sign_in", "/account/login",
	}
	loginParams := []string{"?next=", "?redirect=", "?returnurl=", "?return_to=", "?continue="}

	for _, p := range loginPaths {
		if strings.Contains(urlLower, p) {
			result.Detected = true
			break
		}
	}
	if !result.Detected {
		for _, p := range loginParams {
			if strings.Contains(urlLower, p) {
				result.Detected = true
				break
			}
		}
	}

	// ── Heuristic 2: Page title ───────────────────────────────────────────────
	if !result.Detected {
		title := strings.ToLower(info.Title)
		for _, kw := range []string{"log in", "sign in", "login", "log-in", "sign-in"} {
			if strings.Contains(title, kw) {
				result.Detected = true
				break
			}
		}
	}

	// ── Heuristic 3: DOM — password input field ───────────────────────────────
	// page.Has is non-blocking: it returns immediately based on current DOM state.
	// page.Element would block/retry indefinitely until the element appears.
	if hasPW, pwEl, err := page.Has(`input[type="password"]`); err == nil && hasPW && pwEl != nil {
		result.Detected = true
		result.PasswordInputSelector = `input[type="password"]`

		// Try to find a sibling username/email field within the same form.
		usernameSelectors := []string{
			`input[type="email"]`,
			`input[name="email"]`,
			`input[name="username"]`,
			`input[name="login"]`,
			`input[name="identifier"]`,
			`input[name="user"]`,
			`input[autocomplete="username"]`,
			`input[autocomplete="email"]`,
		}
		for _, sel := range usernameSelectors {
			if has, _, err := page.Has(sel); err == nil && has {
				result.UsernameInputSelector = sel
				break
			}
		}

		// Try to locate the enclosing form.
		res, err := page.Eval(`() => {
			const pw = document.querySelector('input[type="password"]');
			if (!pw) return "";
			const form = pw.closest("form");
			if (!form) return "";
			if (form.id) return "#" + CSS.escape(form.id);
			if (form.className) return "form." + CSS.escape(form.className.trim().split(/\s+/)[0]);
			return "form";
		}`)
		if err == nil && res != nil {
			sel := res.Value.String()
			// rod returns the string "null" when JS returns null; treat as empty.
			if sel != "" && sel != "null" {
				result.FormSelector = sel
			}
		}
	}

	return result
}
