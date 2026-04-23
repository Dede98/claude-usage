package main

type UsageSnapshot struct {
	Version   int                          `json:"version"`
	Timestamp int64                        `json:"timestamp"`
	Providers map[string]*ProviderSnapshot `json:"providers"`
}

type ProviderSnapshot struct {
	Timestamp int64           `json:"timestamp"`
	Source    string          `json:"source"`
	Auth      *ProviderAuth   `json:"auth,omitempty"`
	Limits    *ProviderLimits `json:"limits,omitempty"`
	Error     *string         `json:"error"`
}

type ProviderAuth struct {
	AccountType      string `json:"account_type,omitempty"`
	SubscriptionType string `json:"subscription_type,omitempty"`
	RateLimitTier    string `json:"rate_limit_tier,omitempty"`
	PlanType         string `json:"plan_type,omitempty"`
	Email            string `json:"email,omitempty"`
	TokenExpiresAt   int64  `json:"token_expires_at,omitempty"`
}

type ProviderLimits struct {
	Primary              *WindowInfo                `json:"primary,omitempty"`
	Secondary            *WindowInfo                `json:"secondary,omitempty"`
	Overage              *OverageInfo               `json:"overage,omitempty"`
	Status               string                     `json:"status,omitempty"`
	RepresentativeClaim  string                     `json:"representative_claim,omitempty"`
	Fallback             string                     `json:"fallback,omitempty"`
	LimitID              string                     `json:"limit_id,omitempty"`
	LimitName            string                     `json:"limit_name,omitempty"`
	Credits              *CreditsInfo               `json:"credits,omitempty"`
	RateLimitReachedType string                     `json:"rate_limit_reached_type,omitempty"`
	Buckets              map[string]*ProviderBucket `json:"buckets,omitempty"`
}

type ProviderBucket struct {
	LimitID              string       `json:"limit_id,omitempty"`
	LimitName            string       `json:"limit_name,omitempty"`
	Primary              *WindowInfo  `json:"primary,omitempty"`
	Secondary            *WindowInfo  `json:"secondary,omitempty"`
	Credits              *CreditsInfo `json:"credits,omitempty"`
	PlanType             string       `json:"plan_type,omitempty"`
	RateLimitReachedType string       `json:"rate_limit_reached_type,omitempty"`
}

type WindowInfo struct {
	Utilization    *float64 `json:"utilization,omitempty"`
	UtilizationPct *int     `json:"utilization_pct,omitempty"`
	ResetsAt       *int64   `json:"resets_at,omitempty"`
	ResetsAtISO    *string  `json:"resets_at_iso,omitempty"`
	WindowMinutes  *int64   `json:"window_minutes,omitempty"`
}

type OverageInfo struct {
	Status      string   `json:"status"`
	Utilization *float64 `json:"utilization,omitempty"`
}

type CreditsInfo struct {
	HasCredits bool    `json:"has_credits"`
	Unlimited  bool    `json:"unlimited"`
	Balance    *string `json:"balance,omitempty"`
}
