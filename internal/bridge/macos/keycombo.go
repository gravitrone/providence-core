//go:build darwin

package macos

import (
	"fmt"
	"strings"
)

var keyComboModifierAliases = map[string]string{
	"command": "cmd",
	"cmd":     "cmd",
	"control": "control",
	"ctrl":    "control",
	"option":  "option",
	"alt":     "option",
	"shift":   "shift",
}

var keyComboModifierOrder = []string{"cmd", "control", "option", "shift"}

var keyVirtualCodes = map[string]int{
	"a":         0,
	"s":         1,
	"d":         2,
	"f":         3,
	"h":         4,
	"g":         5,
	"z":         6,
	"x":         7,
	"c":         8,
	"v":         9,
	"b":         11,
	"q":         12,
	"w":         13,
	"e":         14,
	"r":         15,
	"y":         16,
	"t":         17,
	"1":         18,
	"2":         19,
	"3":         20,
	"4":         21,
	"6":         22,
	"5":         23,
	"9":         25,
	"7":         26,
	"8":         28,
	"0":         29,
	"o":         31,
	"u":         32,
	"i":         34,
	"p":         35,
	"return":    36,
	"enter":     36,
	"l":         37,
	"j":         38,
	"k":         40,
	"tab":       48,
	"space":     49,
	"delete":    51,
	"backspace": 51,
	"escape":    53,
	"esc":       53,
	"f5":        96,
	"f6":        97,
	"f7":        98,
	"f3":        99,
	"f8":        100,
	"f9":        101,
	"f11":       103,
	"f10":       109,
	"f12":       111,
	"home":      115,
	"pageup":    116,
	"f4":        118,
	"end":       119,
	"f2":        120,
	"pagedown":  121,
	"f1":        122,
	"left":      123,
	"right":     124,
	"down":      125,
	"up":        126,
}

var appleScriptKeyCodeKeys = map[string]struct{}{
	"return":    {},
	"enter":     {},
	"tab":       {},
	"space":     {},
	"delete":    {},
	"backspace": {},
	"escape":    {},
	"esc":       {},
	"up":        {},
	"down":      {},
	"left":      {},
	"right":     {},
	"home":      {},
	"end":       {},
	"pageup":    {},
	"pagedown":  {},
	"f1":        {},
	"f2":        {},
	"f3":        {},
	"f4":        {},
	"f5":        {},
	"f6":        {},
	"f7":        {},
	"f8":        {},
	"f9":        {},
	"f10":       {},
	"f11":       {},
	"f12":       {},
}

// KeyCombo is a parsed keyboard combo with canonicalized modifiers.
type KeyCombo struct {
	Key         string
	Modifiers   []string
	VirtualCode int
}

// ParseKeyCombo parses a key combo string into a canonical key and modifiers.
func ParseKeyCombo(s string) (KeyCombo, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return KeyCombo{}, fmt.Errorf("empty key combo")
	}

	parts := strings.Split(trimmed, "+")
	seenModifiers := make(map[string]bool, len(keyComboModifierOrder))

	var key string
	var nonModifierCount int

	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			return KeyCombo{}, fmt.Errorf("no key specified in combo: %s", s)
		}

		if modifier, ok := keyComboModifierAliases[part]; ok {
			seenModifiers[modifier] = true
			continue
		}

		key = part
		nonModifierCount++
	}

	if nonModifierCount == 0 {
		return KeyCombo{}, fmt.Errorf("no key specified in combo: %s", s)
	}

	var modifiers []string
	if len(seenModifiers) > 0 {
		modifiers = make([]string, 0, len(seenModifiers))
		for _, modifier := range keyComboModifierOrder {
			if seenModifiers[modifier] {
				modifiers = append(modifiers, modifier)
			}
		}
	}

	virtualCode := -1
	if code, ok := keyVirtualCodes[key]; ok {
		virtualCode = code
	}

	return KeyCombo{
		Key:         key,
		Modifiers:   modifiers,
		VirtualCode: virtualCode,
	}, nil
}

func usesAppleScriptKeyCode(key string) bool {
	_, ok := appleScriptKeyCodeKeys[key]
	return ok
}
