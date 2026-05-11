package tui

import (
	"math/rand"
	"regexp"
	"strings"
)

var (
	greetingRE = regexp.MustCompile(`^(?i)(hi|hello|hey|howdy|good\s?(morning|afternoon|evening|day)|greetings|sup|yo|what'?s\s?up|hola)[\s!.]*$`)

	thanksRE = regexp.MustCompile(`^(?i)(thanks|thank\s?you|thank|thx|ty|cheers|appreciate\s?it)[\s!.]*$`)

	greetingResponses = []string{
		"Hello! How can I help you?",
		"Hi there! What can I do for you?",
		"Hey! How can I assist you today?",
		"Hello! I'm here to help. What do you need?",
	}

	thanksResponses = []string{
		"You're welcome!",
		"Happy to help!",
		"Anytime! Let me know if you need anything else.",
		"My pleasure!",
	}
)

func AutoReply(prompt string) (string, bool) {
	p := strings.TrimSpace(prompt)
	switch {
	case greetingRE.MatchString(p):
		return greetingResponses[rand.Intn(len(greetingResponses))], true
	case thanksRE.MatchString(p):
		return thanksResponses[rand.Intn(len(thanksResponses))], true
	}
	return "", false
}
