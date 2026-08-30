package webhook

import "strings"

func ValidSubjectFilter(filter string) bool {
	if !strings.HasPrefix(filter, "platform.") || strings.HasPrefix(filter, "platform.webhook.") {
		return false
	}
	parts := strings.Split(filter, ".")
	for index, part := range parts {
		if part == "" || (strings.ContainsAny(part, "*>") && part != "*" && part != ">") {
			return false
		}
		if part == ">" && index != len(parts)-1 {
			return false
		}
	}
	return true
}

func SubjectMatches(filter, subject string) bool {
	if !ValidSubjectFilter(filter) || strings.HasPrefix(subject, "platform.webhook.") {
		return false
	}
	filterParts := strings.Split(filter, ".")
	subjectParts := strings.Split(subject, ".")
	for index, part := range filterParts {
		if part == ">" {
			return index < len(subjectParts)
		}
		if index >= len(subjectParts) || (part != "*" && part != subjectParts[index]) {
			return false
		}
	}
	return len(filterParts) == len(subjectParts)
}
