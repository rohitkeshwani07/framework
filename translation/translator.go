package translation

import (
	"context"
	"strconv"
	"strings"

	"github.com/spf13/cast"
	"golang.org/x/exp/slices"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/goravel/framework/contracts/http"
	logcontract "github.com/goravel/framework/contracts/log"
	translationcontract "github.com/goravel/framework/contracts/translation"
)

type Translator struct {
	ctx      context.Context
	loader   translationcontract.Loader
	locale   string
	fallback string
	// loaded is a map structure used to store loaded translation data.
	// It is organized as follows:
	//   - First map (map[string]): Maps from locale to...
	//     - Second map (map[string]): Maps from folder(group) to...
	//       - Third map (map[string]): Maps from key to...
	//         - Value (any): The translation line corresponding to the key in the specified locale, folder(group), and key hierarchy.
	loaded   map[string]map[string]map[string]any
	selector *MessageSelector
	key      string
	logger   logcontract.Log
}

// contextKey is an unexported type for keys defined in this package.
type contextKey string

const fallbackLocaleKey = contextKey("fallback_locale")
const localeKey = contextKey("locale")

func NewTranslator(ctx context.Context, loader translationcontract.Loader, locale string, fallback string, logger logcontract.Log) *Translator {
	return &Translator{
		ctx:      ctx,
		loader:   loader,
		locale:   locale,
		fallback: fallback,
		loaded:   make(map[string]map[string]map[string]any),
		selector: NewMessageSelector(),
		logger:   logger,
	}
}

func (t *Translator) Choice(key string, number int, options ...translationcontract.Option) string {
	line := t.Get(key, options...)

	replace := map[string]string{
		"count": strconv.Itoa(number),
	}

	locale := t.GetLocale()
	if len(options) > 0 && options[0].Locale != "" {
		locale = options[0].Locale
	}

	return makeReplacements(t.selector.Choose(line, number, locale), replace)
}

func (t *Translator) Get(key string, options ...translationcontract.Option) string {
	if t.key == "" {
		t.key = key
	}

	locale := t.GetLocale()
	// Check if a custom locale is provided in options.
	if len(options) > 0 && options[0].Locale != "" {
		locale = options[0].Locale
	}

	fallback := true
	// Check if a custom fallback is provided in options.
	if len(options) > 0 && options[0].Fallback != nil {
		fallback = *options[0].Fallback
	}

	// For JSON translations, there is only one file per locale, so we will
	// simply load the file and return the line if it exists.
	// If the file doesn't exist, we will return fallback if it is enabled.
	// Otherwise, we will return the key as the line.
	if err := t.load(locale, "*"); err != nil && err != ErrFileNotExist {
		t.logger.Panic(err)
		return t.key
	}

	keyValue := getValue(t.loaded[locale]["*"], key)
	// if the key is not found `{locale}.json` file, we will try to find it in
	// the group files.
	if keyValue != nil {
		line := cast.ToString(keyValue)
		if line == "" {
			return t.key
		}

		// If the line doesn't contain any placeholders, we can return it right
		// away.Otherwise, we will make the replacements on the line and return
		// the result.
		if len(options) > 0 {
			return makeReplacements(line, options[0].Replace)
		}

		return line
	}

	// Parse the key into group and item.
	group, item := parseKey(key)

	// Here we will get the locale that should be used for the language line.
	// If it doesn't exist, we will use the default locale which was given
	// to us when the translator was instantiated.Then we can load the lines
	// and return the value.
	locales := []string{locale}
	if fallback {
		locales = t.appendFallbackLocale(locales)
	}

	for _, loc := range locales {
		line := t.getLine(loc, group, item, options...)
		if line != "" {
			return line
		}
	}

	return t.key
}

func (t *Translator) GetFallback() string {
	if fallback, ok := t.ctx.Value(string(fallbackLocaleKey)).(string); ok {
		return fallback
	}
	return t.fallback
}

func (t *Translator) GetLocale() string {
	if locale, ok := t.ctx.Value(string(localeKey)).(string); ok {
		return locale
	}
	return t.locale
}

func (t *Translator) Has(key string, options ...translationcontract.Option) bool {
	line := t.Get(key, options...)
	return line != key
}

func (t *Translator) SetFallback(locale string) context.Context {
	t.fallback = locale
	//nolint:all
	t.ctx = context.WithValue(t.ctx, string(fallbackLocaleKey), locale)

	return t.ctx
}

func (t *Translator) SetLocale(locale string) context.Context {
	t.locale = locale
	if ctx, ok := t.ctx.(http.Context); ok {
		ctx.WithValue(string(localeKey), locale)
		t.ctx = ctx
	} else {
		//nolint:all
		t.ctx = context.WithValue(t.ctx, string(localeKey), locale)
	}
	return t.ctx
}

func (t *Translator) getLine(locale string, group string, key string, options ...translationcontract.Option) string {
	if err := t.load(locale, group); err != nil && err != ErrFileNotExist {
		t.logger.Panic(err)
		return t.key
	}

	keyValue := getValue(t.loaded[locale][group], key)
	if keyValue == nil {
		return ""
	}

	line := cast.ToString(keyValue)
	if len(options) > 0 {
		return makeReplacements(line, options[0].Replace)
	}

	return line
}

func (t *Translator) load(locale string, group string) error {
	if t.isLoaded(locale, group) {
		return nil
	}

	translations, err := t.loader.Load(locale, group)
	if err != nil {
		return err
	}
	if t.loaded[locale] == nil {
		t.loaded[locale] = make(map[string]map[string]any)
	}
	t.loaded[locale][group] = translations
	return nil
}

func (t *Translator) appendFallbackLocale(locales []string) []string {
	fallbackLocale := t.GetFallback()
	if fallbackLocale != "" && !slices.Contains(locales, fallbackLocale) {
		locales = append(locales, fallbackLocale)
	}
	return locales
}

func (t *Translator) isLoaded(locale string, group string) bool {
	if _, ok := t.loaded[locale]; !ok {
		return false
	}

	if _, ok := t.loaded[locale][group]; !ok {
		return false
	}

	return true
}

func makeReplacements(line string, replace map[string]string) string {
	if len(replace) == 0 {
		return line
	}

	var shouldReplace []string
	casesTitle := cases.Title(language.Und)
	for k, v := range replace {
		shouldReplace = append(shouldReplace, ":"+k, v)
		shouldReplace = append(shouldReplace, ":"+casesTitle.String(k), casesTitle.String(v))
		shouldReplace = append(shouldReplace, ":"+strings.ToUpper(k), strings.ToUpper(v))
	}

	return strings.NewReplacer(shouldReplace...).Replace(line)
}

// parseKey parses a key into group and item.
func parseKey(key string) (group, item string) {
	segments := strings.Split(key, ".")

	group = segments[0]

	if len(segments) == 1 {
		item = ""
	} else {
		item = strings.Join(segments[1:], ".")
	}

	return group, item
}

// getValue an item from an object using "dot" notation.
func getValue(obj any, key string) any {
	keys := strings.Split(key, ".")

	var currentObj any
	currentObj = obj

	for _, k := range keys {
		switch v := currentObj.(type) {
		case map[string]any:
			if val, found := v[k]; found {
				currentObj = val
			} else {
				return nil
			}
		default:
			return nil
		}
	}

	return currentObj
}
