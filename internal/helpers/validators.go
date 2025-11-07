package helpers

import (
	"fmt"
	"regexp"
	"strings"
)

func IsValidEmail(email string) bool {
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	return emailRegex.MatchString(email)
}

func IsValidDomain(domain string) error {
	if len(domain) == 0 || len(domain) > 253 {
		return fmt.Errorf("domain length must be between 1 and 253 characters")
	}

	// Check for invalid characters at start/end
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return fmt.Errorf("domain cannot start or end with a dot")
	}

	if strings.HasPrefix(domain, "-") || strings.HasSuffix(domain, "-") {
		return fmt.Errorf("domain cannot start or end with a hyphen")
	}

	// Split into labels and validate each
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return fmt.Errorf("domain must have at least two labels (e.g., example.com)")
	}

	for i, label := range labels {
		if i == len(labels)-1 {
			// Special validation for TLD (last label)
			if err := validateTLD(label); err != nil {
				return fmt.Errorf("invalid TLD '%s': %w", label, err)
			}
		} else {
			// Regular label validation for non-TLD labels
			if err := validateDomainLabel(label); err != nil {
				return fmt.Errorf("invalid label '%s': %w", label, err)
			}
		}
	}

	return nil
}

func validateDomainLabel(label string) error {
	if len(label) == 0 || len(label) > 63 {
		return fmt.Errorf("label length must be between 1 and 63 characters")
	}

	if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
		return fmt.Errorf("label cannot start or end with hyphen")
	}

	// Check for valid characters (alphanumeric and hyphens)
	for _, r := range label {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-') {
			return fmt.Errorf("label contains invalid character: %c", r)
		}
	}

	return nil
}

func validateTLD(tld string) error {
	// TLD must be at least 2 characters long (ICANN policy)
	if len(tld) < 2 || len(tld) > 63 {
		return fmt.Errorf("TLD length must be between 2 and 63 characters")
	}

	// TLD cannot start or end with hyphen
	if strings.HasPrefix(tld, "-") || strings.HasSuffix(tld, "-") {
		return fmt.Errorf("TLD cannot start or end with hyphen")
	}

	// TLD should only contain letters (no numbers or hyphens in practice)
	// However, some newer TLDs do contain numbers, so we'll allow alphanumeric
	for _, r := range tld {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9')) {
			return fmt.Errorf("TLD contains invalid character: %c", r)
		}
	}

	return nil
}
