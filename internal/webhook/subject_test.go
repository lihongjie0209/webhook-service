package webhook

import "testing"

func TestSubjectFilterAndMatch(t *testing.T) {
	tests := []struct {
		name    string
		filter  string
		subject string
		valid   bool
		match   bool
	}{
		{name: "exact", filter: "platform.identity.user.created.v1", subject: "platform.identity.user.created.v1", valid: true, match: true},
		{name: "single token", filter: "platform.identity.*.created.v1", subject: "platform.identity.user.created.v1", valid: true, match: true},
		{name: "tail", filter: "platform.identity.>", subject: "platform.identity.user.created.v1", valid: true, match: true},
		{name: "tail requires token", filter: "platform.identity.>", subject: "platform.identity", valid: true},
		{name: "different", filter: "platform.tenant.>", subject: "platform.identity.user.created.v1", valid: true},
		{name: "embedded wildcard", filter: "platform.ident*.>", subject: "platform.identity.user", valid: false},
		{name: "tail not last", filter: "platform.>.created", subject: "platform.user.created", valid: false},
		{name: "non-platform", filter: "external.>", subject: "external.event", valid: false},
		{name: "recursive webhook", filter: "platform.webhook.>", subject: "platform.webhook.delivery.succeeded.v1", valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidSubjectFilter(test.filter); got != test.valid {
				t.Fatalf("ValidSubjectFilter() = %v", got)
			}
			if got := SubjectMatches(test.filter, test.subject); got != test.match {
				t.Fatalf("SubjectMatches() = %v", got)
			}
		})
	}
}
