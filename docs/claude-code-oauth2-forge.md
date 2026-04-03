# Claude Code Prompt — OAuth2 Client Credentials for Forge (AuthCore v2)

## Context

Project Atlas is a local AI operator with a Go runtime and web-first control surface. The Forge pipeline lets the agent propose
and install custom HTTP API skills at runtime. Each skill is described by a
`ForgeSkillPackage` containing a `SkillManifest` and an array of `ForgeActionPlan`
objects. At runtime, `ForgeSkill.executeHTTP` reads Keychain credentials and injects
them into each HTTP request according to `HTTPRequestPlan.authType`.

The auth system is in `atlas-skills/Sources/AtlasSkills/CoreSkills/`. The key files are:

- `AuthCore.swift` — `APIAuthType` enum + `AuthCore` support matrix + refusal messages
- `ForgeSkillPackage.swift` — `HTTPRequestPlan` struct with all auth fields
- `ForgeSkill.swift` — runtime HTTP execution, auth injection, Keychain reads
- `ForgeValidationGate.swift` — Gates 1–7: contract quality + auth plan completeness
- `ForgeCredentialGate.swift` — Gate 8: Keychain credential presence check
- `ForgeOrchestrationSkill.swift` — agent-facing skill: system prompt, proposal creation

**Current state:** `oauth2ClientCredentials` exists in `APIAuthType` but returns
`.requiresFutureOAuthSupport` from `AuthCore.supportLevel(for:)`, causing Gate 6 to
hard-refuse any proposal that uses it.

## Goal

Add full support for `oauth2AuthorizationCode` stays refused — do not touch it.
Add full support for **OAuth 2.0 Client Credentials** (`oauth2ClientCredentials`) so that
Forge skills can call APIs that require server-to-server token exchange (Spotify,
Salesforce, Slack bot tokens, etc.).

`oauth2AuthorizationCode` **must remain refused** — it requires a browser redirect
and callback server that Atlas does not have.

---

## Required changes — implement all of the following

### 1. New file: `OAuth2TokenCache.swift`

Create at `atlas-skills/Sources/AtlasSkills/CoreSkills/OAuth2TokenCache.swift`.

```swift
import Foundation

/// Process-scoped in-memory token cache for OAuth 2.0 Client Credentials tokens.
///
/// Tokens are keyed by "\(tokenURL)|\(clientIDKey)" to allow multiple APIs to share
/// the same cache without collision.
///
/// Thread-safety: implemented as a Swift actor so concurrent `ForgeSkill` executions
/// do not race on cache reads/writes.
///
/// Security: access tokens are NEVER logged. Expiry uses a 60-second safety margin
/// so tokens are never used in the final minute of their validity window.
public actor OAuth2TokenCache {
    public static let shared = OAuth2TokenCache()

    private struct CachedToken {
        let accessToken: String
        let expiresAt: Date
    }

    private var cache: [String: CachedToken] = [:]

    private init() {}

    /// Returns a valid cached token for `key`, or nil if absent or expired.
    public func token(for key: String) -> String? {
        guard let entry = cache[key], Date() < entry.expiresAt else {
            cache.removeValue(forKey: key)
            return nil
        }
        return entry.accessToken
    }

    /// Stores a token with a 60-second safety margin on the expiry.
    public func store(token: String, expiresIn: Int, for key: String) {
        let expiresAt = Date().addingTimeInterval(TimeInterval(expiresIn) - 60)
        cache[key] = CachedToken(accessToken: token, expiresAt: expiresAt)
    }

    /// Evicts a single entry — call this on a 401 to force a fresh token exchange.
    public func invalidate(for key: String) {
        cache.removeValue(forKey: key)
    }
}
```

### 2. New file: `OAuth2ClientCredentialsService.swift`

Create at `atlas-skills/Sources/AtlasSkills/CoreSkills/OAuth2ClientCredentialsService.swift`.

```swift
import Foundation

public enum OAuth2Error: LocalizedError, Sendable {
    case missingTokenURL
    case missingClientID
    case missingClientSecret
    case tokenEndpointFailed(Int, String?)
    case missingAccessToken

    public var errorDescription: String? {
        switch self {
        case .missingTokenURL:      return "oauth2ClientCredentials plan is missing oauth2TokenURL."
        case .missingClientID:      return "oauth2ClientCredentials plan is missing oauth2ClientIDKey."
        case .missingClientSecret:  return "oauth2ClientCredentials plan is missing oauth2ClientSecretKey."
        case .tokenEndpointFailed(let status, let body):
            return "Token endpoint returned HTTP \(status)\(body.map { ": \($0)" } ?? "")."
        case .missingAccessToken:   return "Token endpoint response did not contain access_token."
        }
    }
}

/// Fetches and caches OAuth 2.0 Client Credentials tokens for use in Forge skills.
///
/// Stateless — all state lives in `OAuth2TokenCache.shared`.
/// Injected with `CoreHTTPService` and `CoreSecretsService` so secrets never leave Keychain.
public struct OAuth2ClientCredentialsService: Sendable {
    private let httpService: CoreHTTPService
    private let secretsService: CoreSecretsService

    public init(httpService: CoreHTTPService, secretsService: CoreSecretsService) {
        self.httpService = httpService
        self.secretsService = secretsService
    }

    /// Returns a valid access token for the given plan, using the cache when possible.
    ///
    /// Cache key is "\(oauth2TokenURL)|\(oauth2ClientIDKey)" — unique per API + credential pair.
    public func fetchToken(plan: HTTPRequestPlan) async throws -> String {
        guard let tokenURL = plan.oauth2TokenURL, !tokenURL.isEmpty else {
            throw OAuth2Error.missingTokenURL
        }
        guard let clientIDKey = plan.oauth2ClientIDKey, !clientIDKey.isEmpty else {
            throw OAuth2Error.missingClientID
        }
        guard let clientSecretKey = plan.oauth2ClientSecretKey, !clientSecretKey.isEmpty else {
            throw OAuth2Error.missingClientSecret
        }

        let cacheKey = "\(tokenURL)|\(clientIDKey)"
        if let cached = await OAuth2TokenCache.shared.token(for: cacheKey) {
            return cached
        }

        guard let clientID = try await secretsService.get(service: clientIDKey) else {
            throw OAuth2Error.missingClientID
        }
        guard let clientSecret = try await secretsService.get(service: clientSecretKey) else {
            throw OAuth2Error.missingClientSecret
        }

        return try await exchangeClientCredentials(
            tokenURL: tokenURL,
            clientID: clientID,
            clientSecret: clientSecret,
            cacheKey: cacheKey,
            scope: plan.oauth2Scope
        )
    }

    private func exchangeClientCredentials(
        tokenURL: String,
        clientID: String,
        clientSecret: String,
        cacheKey: String,
        scope: String?
    ) async throws -> String {
        guard let url = URL(string: tokenURL) else {
            throw OAuth2Error.missingTokenURL
        }

        var formFields = "grant_type=client_credentials"
            + "&client_id=\(urlEncode(clientID))"
            + "&client_secret=\(urlEncode(clientSecret))"
        if let scope, !scope.isEmpty {
            formFields += "&scope=\(urlEncode(scope))"
        }

        let request = CoreHTTPRequest(
            url: url,
            method: .post,
            headers: ["Content-Type": "application/x-www-form-urlencoded"],
            body: formFields.data(using: .utf8)
        )

        let response = try await httpService.execute(request)
        guard response.isSuccess else {
            throw OAuth2Error.tokenEndpointFailed(response.statusCode, response.bodyString.map { String($0.prefix(200)) })
        }

        guard
            let data = response.bodyString?.data(using: .utf8),
            let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
            let accessToken = json["access_token"] as? String, !accessToken.isEmpty
        else {
            throw OAuth2Error.missingAccessToken
        }

        let expiresIn = (json["expires_in"] as? Int) ?? 3600
        await OAuth2TokenCache.shared.store(token: accessToken, expiresIn: expiresIn, for: cacheKey)
        return accessToken
    }

    private func urlEncode(_ s: String) -> String {
        s.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? s
    }
}
```

### 3. Modify `ForgeSkillPackage.swift` — add three fields to `HTTPRequestPlan`

Add these three stored properties **after** `authQueryParamName` and **before** `secretHeader`:

```swift
// MARK: Auth Fields — OAuth 2.0 Client Credentials

/// For `oauth2ClientCredentials` only: the token endpoint URL.
/// E.g. "https://accounts.spotify.com/api/token"
public let oauth2TokenURL: String?

/// For `oauth2ClientCredentials` only: Keychain key for the client_id.
/// E.g. "com.projectatlas.spotify.clientid"
public let oauth2ClientIDKey: String?

/// For `oauth2ClientCredentials` only: Keychain key for the client_secret.
/// E.g. "com.projectatlas.spotify.secret"
public let oauth2ClientSecretKey: String?

/// For `oauth2ClientCredentials` only: optional OAuth scope string.
/// E.g. "read:metrics write:metrics"
public let oauth2Scope: String?
```

Add them to `init` with `nil` defaults. Update the auth table in the doc comment to include:

```
/// | `oauth2ClientCredentials` | `oauth2TokenURL`, `oauth2ClientIDKey`, `oauth2ClientSecretKey` | POST token endpoint → `Authorization: Bearer <access_token>` |
```

### 4. Modify `AuthCore.swift`

In `supportLevel(for:)`, move `oauth2ClientCredentials` from the `.requiresFutureOAuthSupport`
case to the `.supported` case — it now belongs alongside the other supported types.

Remove the `oauth2ClientCredentials` case from `refusalMessage(for:skillName:)` — it is
no longer refused. Update the `AuthCore` doc comment to add `oauth2ClientCredentials` to
the "Supported" table with the note: "POST to `oauth2TokenURL` → cache → `Authorization: Bearer`".

Update the `oauth2AuthorizationCode` refusal message to add one sentence at the end:
"If this API offers a service-account or machine-to-machine option (Client Credentials),
that is supported — set `authType` to `\"oauth2ClientCredentials\"` instead."

Remove the "Deferred to AuthCore v2" bullet for `oauth2ClientCredentials`. Keep it only
for `oauth2AuthorizationCode`.

### 5. Modify `ForgeValidationGate.swift`

In `evaluatePlans(_:skillName:)`, add a case for `.oauth2ClientCredentials` in the
`authType` switch. Check that all three required fields are present and non-empty:
`oauth2TokenURL`, `oauth2ClientIDKey`, `oauth2ClientSecretKey`. Return
`.needsClarification` listing which fields are missing, using the same
`missingField(_:_:)` helper pattern already used for other auth types.

Remove `oauth2ClientCredentials` from the "should have been blocked by Gate 6" comment
since it is now a valid, supported type.

### 6. Modify `ForgeCredentialGate.swift`

Add `.oauth2ClientCredentials` to the `credentialTypes` set.

For `oauth2ClientCredentials` plans, validate **both** `oauth2ClientIDKey` and
`oauth2ClientSecretKey` (not `authSecretKey`, which is used by v1 auth types only).
If either key is missing from Keychain, append a descriptive entry to `missing[]`
explaining which credential is needed and what Keychain key name to use.

### 7. Modify `ForgeSkill.swift`

Add `private let oauth2Service: OAuth2ClientCredentialsService` as a stored property.
Initialise it in `init` from the existing `httpService` and `secretsService`.

In `executeHTTP`, add a case for `.oauth2ClientCredentials` in the `authType` switch
**before** the existing unsupported-types throw:

```swift
case .oauth2ClientCredentials:
    let token = try await oauth2Service.fetchToken(plan: plan)
    headers["Authorization"] = "Bearer \(token)"
```

Add a 401-retry path: after `httpService.execute(request)`, if
`response.statusCode == 401 && plan.authType == .oauth2ClientCredentials`:
1. Build the cache key: `"\(plan.oauth2TokenURL ?? "")|\(plan.oauth2ClientIDKey ?? "")"`
2. Call `await OAuth2TokenCache.shared.invalidate(for: cacheKey)`
3. Fetch a fresh token via `oauth2Service.fetchToken(plan: plan)`
4. Rebuild the `Authorization` header
5. Re-execute the HTTP request once
6. Return the result of the retry — do not retry again on a second 401

Remove `oauth2ClientCredentials` from the "unsupported auth type" throw branch — it is
handled above.

### 8. Modify `ForgeOrchestrationSkill.swift`

**System prompt block (`systemPromptBlock`):**

In the "Auth type classification" section, replace:
```
- "oauth2ClientCredentials" — OAuth server-to-server → NOT supported, Forge will refuse
```
with:
```
- "oauth2ClientCredentials" — OAuth 2.0 server-to-server (Client Credentials) → SUPPORTED.
  Use for APIs that issue tokens via a token endpoint (Spotify, Salesforce, Slack, etc.).
  Requires oauth2TokenURL, oauth2ClientIDKey, oauth2ClientSecretKey.
  Optionally include oauth2Scope if the API requires a scope parameter.
```

Add to the "Contract quality gates" section, after the `authType` bullet:
```
  → oauth2ClientCredentials is fully supported; use it for machine-to-machine flows
```

In the plans_json auth examples section, add:
```
- oauth2ClientCredentials: {"authType":"oauth2ClientCredentials","oauth2TokenURL":"https://accounts.spotify.com/api/token","oauth2ClientIDKey":"com.projectatlas.spotify.clientid","oauth2ClientSecretKey":"com.projectatlas.spotify.secret","oauth2Scope":"user-read-playback-state"}
```

Update the full plan example's `plans_json` description to document the four new
optional fields: `oauth2TokenURL`, `oauth2ClientIDKey`, `oauth2ClientSecretKey`,
`oauth2Scope`.

**Manifest version:** bump from `"1.2.0"` to `"1.3.0"`.

---

## Constraints — read these before writing any code

1. **`oauth2AuthorizationCode` must remain refused.** Do not change its support level,
   do not add execution logic for it, do not soften its refusal message (except the one
   new sentence pointing to Client Credentials as an alternative).

2. **Secrets are never logged.** `OAuth2ClientCredentialsService` must not log
   `clientID`, `clientSecret`, or `accessToken` at any log level. Log only structural
   facts (token endpoint host, HTTP status, whether the cache was hit).

3. **No new Swift packages.** All new files go inside
   `atlas-skills/Sources/AtlasSkills/CoreSkills/`. No changes to `Package.swift`.

4. **Backward compatibility.** Existing Forge skills that use `authType = nil`
   (legacy `secretHeader` path) or any v1 auth type must continue to work identically.
   No breaking changes to `HTTPRequestPlan`, `ForgeActionPlan`, or `ForgeSkillPackage`
   — all new fields have `nil` defaults.

5. **401 retry is bounded.** The retry in `ForgeSkill.executeHTTP` executes at most
   once. A second 401 returns the failed `SkillExecutionResult` immediately — no loop.

6. **Token cache is in-memory only.** `OAuth2TokenCache` uses no persistence layer.
   Tokens survive only for the daemon process lifetime. This is correct and intentional.

7. **Compile clean.** After all changes, `swift build` on both `atlas-skills` and
   `atlas-core` (which imports `atlas-skills`) must succeed with zero errors. Warnings
   are acceptable only if they existed before this change.

8. **Follow existing code style exactly.** Observe the conventions in the files you're
   editing: `// MARK: -` section separators, doc comments on all public symbols,
   `Sendable` conformance on all new value types and actors, structured logging via
   `AtlasLogger` (category `"forge.oauth2"`), and errors as `LocalizedError` enums.

---

## Verification checklist

After implementing, verify each of the following:

- [ ] `swift build` on `atlas-skills` — zero errors
- [ ] `swift build` on `atlas-core` — zero errors
- [ ] `AuthCore.supportLevel(for: .oauth2ClientCredentials)` returns `.supported`
- [ ] `AuthCore.supportLevel(for: .oauth2AuthorizationCode)` still returns `.requiresFutureOAuthSupport`
- [ ] `ForgeValidationGate().evaluate(contract:skillName:)` passes for a contract with `authType: .oauth2ClientCredentials`
- [ ] `ForgeValidationGate().evaluatePlans(_:skillName:)` returns `.needsClarification` when `oauth2TokenURL` is missing
- [ ] `ForgeValidationGate().evaluatePlans(_:skillName:)` returns `.pass` when all three fields are present
- [ ] `ForgeCredentialGate().evaluate(plans:skillName:secrets:)` returns `.needsClarification` when `oauth2ClientIDKey` is not in Keychain
- [ ] `OAuth2TokenCache` returns `nil` on miss and the stored token on hit (within expiry)
- [ ] `OAuth2TokenCache` returns `nil` after `invalidate(for:)` is called
- [ ] `ForgeOrchestrationSkill` version is `"1.3.0"`
- [ ] No existing test files are broken
