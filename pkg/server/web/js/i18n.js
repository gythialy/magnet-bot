/*
 * i18n.js — the Mini App's bilingual layer.
 *
 * Responsibilities:
 *   - resolve one language per device: localStorage.magnet.lang (auto|zh|en)
 *     -> Telegram initDataUnsafe.user.language_code -> navigator.languages -> en
 *   - expose t(), plural(), num(), date(), time() so views never concatenate
 *     translated fragments
 *   - translate the static shell through data-i18n / data-i18n-ph /
 *     data-i18n-aria / data-i18n-title, in place: switching language must not
 *     rebuild the DOM, so drafts, focus, open sheets and scroll survive
 *   - keep <html lang> and document.title in sync
 *
 * The dictionaries are loaded before this file (js/locales/en.js, zh.js). A
 * missing key falls back to the other dictionary, then to the key itself, so a
 * half-translated build stays readable instead of blank.
 *
 * No build step, no external requests.
 */
(function (global) {
  "use strict";

  var STORAGE_KEY = "magnet.lang";
  var DEFAULT_LANG = "en";
  var SUPPORTED = { en: true, zh: true };
  // Telegram reports a BCP-47 code; every Chinese variant maps to zh.
  var ZH_PREFIXES = ["zh"];

  var listeners = [];
  var state = {
    pref: "auto",
    lang: DEFAULT_LANG,
  };

  function locales() {
    return global.MagnetLocales || {};
  }

  function dict(lang) {
    return locales()[lang] || {};
  }

  function telegramUser() {
    var webApp = global.Telegram && global.Telegram.WebApp;
    if (!webApp || !webApp.initDataUnsafe) {
      return null;
    }
    return webApp.initDataUnsafe.user || null;
  }

  function normalizeLang(code) {
    var raw = String(code || "").trim().toLowerCase();
    if (!raw) {
      return "";
    }
    for (var i = 0; i < ZH_PREFIXES.length; i++) {
      if (raw === ZH_PREFIXES[i] || raw.indexOf(ZH_PREFIXES[i] + "-") === 0) {
        return "zh";
      }
    }
    if (raw === "en" || raw.indexOf("en-") === 0) {
      return "en";
    }
    return "";
  }

  // A device preference wins, but only when it is a language we ship.
  function readStoredPref() {
    try {
      var stored = global.localStorage.getItem(STORAGE_KEY);
      if (stored === "auto" || SUPPORTED[stored]) {
        return stored;
      }
    } catch (err) {
      // Private webviews may refuse localStorage; fall back to auto.
    }
    return "auto";
  }

  function deviceLang() {
    var user = telegramUser();
    var fromTelegram = user ? normalizeLang(user.language_code) : "";
    if (fromTelegram) {
      return fromTelegram;
    }

    var languages = global.navigator && global.navigator.languages;
    if (!languages || !languages.length) {
      languages = [global.navigator && global.navigator.language];
    }
    for (var i = 0; i < languages.length; i++) {
      var match = normalizeLang(languages[i]);
      if (match) {
        return match;
      }
    }

    return DEFAULT_LANG;
  }

  function resolve(pref) {
    return pref === "auto" ? deviceLang() : pref;
  }

  function appLang() {
    return state.lang;
  }

  /* Formatting ----------------------------------------------------------- */

  function hasIntl() {
    return typeof global.Intl === "object" && !!global.Intl.NumberFormat;
  }

  function localeTag() {
    return state.lang === "zh" ? "zh-CN" : "en-US";
  }

  function interpolate(template, vars) {
    if (!vars) {
      return template;
    }
    return String(template).replace(/\{(\w+)\}/g, function (match, name) {
      return Object.prototype.hasOwnProperty.call(vars, name) ? String(vars[name]) : match;
    });
  }

  /**
   * t("lib.addedToast", {count: 3}) — a dictionary string with {tokens} filled
   * from vars. A plural object is resolved through CLDR instead of picked by
   * string concatenation, which is what "N notice" + "s" used to do.
   */
  function t(key, vars) {
    var value = dict(state.lang)[key];
    if (value === undefined) {
      value = dict(state.lang === "zh" ? "en" : "zh")[key];
    }
    if (value === undefined) {
      return String(key);
    }
    if (typeof value === "object" && value !== null) {
      return interpolate(selectPlural(value, countOf(vars)), vars);
    }
    return interpolate(value, vars);
  }

  function countOf(vars) {
    if (vars && typeof vars.n === "number") {
      return vars.n;
    }
    return 0;
  }

  function selectPlural(forms, count) {
    var category = "other";
    if (hasIntl() && global.Intl.PluralRules) {
      try {
        category = new global.Intl.PluralRules(localeTag()).select(count);
      } catch (err) {
        category = "other";
      }
    } else if (count === 1) {
      category = "one";
    }
    if (forms[category] !== undefined) {
      return forms[category];
    }
    if (forms.other !== undefined) {
      return forms.other;
    }
    return "";
  }

  /** The CLDR plural form of a unit phrase: plural("unit.notices", 1). */
  function plural(key, count) {
    var value = dict(state.lang)[key];
    if (value === undefined) {
      value = dict(state.lang === "zh" ? "en" : "zh")[key];
    }
    if (value === undefined) {
      return String(key);
    }
    if (typeof value === "object" && value !== null) {
      return selectPlural(value, count);
    }
    return String(value);
  }

  function num(value) {
    var number = Number(value);
    if (!isFinite(number)) {
      return "0";
    }
    if (!hasIntl()) {
      return String(number);
    }
    try {
      return new global.Intl.NumberFormat(localeTag()).format(number);
    } catch (err) {
      return String(number);
    }
  }

  function parseISO(value) {
    if (!value) {
      return null;
    }
    var date = new Date(value);
    return isNaN(date.getTime()) ? null : date;
  }

  function formatDate(value, options) {
    var date = parseISO(value);
    if (!date) {
      return "";
    }
    if (!hasIntl() || !global.Intl.DateTimeFormat) {
      return date.getFullYear() + "-" + pad2(date.getMonth() + 1) + "-" + pad2(date.getDate());
    }
    try {
      return new global.Intl.DateTimeFormat(localeTag(), options).format(date);
    } catch (err) {
      return date.getFullYear() + "-" + pad2(date.getMonth() + 1) + "-" + pad2(date.getDate());
    }
  }

  function pad2(number) {
    return number < 10 ? "0" + number : String(number);
  }

  /** A calendar date: 2026-09-12 / 2026/09/12. */
  function date(value) {
    return formatDate(value, { year: "numeric", month: "2-digit", day: "2-digit" });
  }

  /** A clock time: 17:42. */
  function time(value) {
    return formatDate(value, { hour: "2-digit", minute: "2-digit", hour12: false });
  }

  /** Month and day for a compact date: 10-31. */
  function monthDay(value) {
    return formatDate(value, { month: "2-digit", day: "2-digit" });
  }

  /** Month/day and time for a notice row: 10-31 08:12. */
  function dateTime(value) {
    return formatDate(value, {
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    });
  }

  /* DOM application ------------------------------------------------------ */

  function lookup(key) {
    var value = dict(state.lang)[key];
    if (value === undefined) {
      value = dict(state.lang === "zh" ? "en" : "zh")[key];
    }
    return typeof value === "string" ? value : undefined;
  }

  function applyNode(root) {
    var scope = root || global.document;
    if (!scope || !scope.querySelectorAll) {
      return;
    }

    Array.prototype.forEach.call(scope.querySelectorAll("[data-i18n]"), function (el) {
      var text = lookup(el.getAttribute("data-i18n"));
      if (text !== undefined) {
        el.textContent = text;
      }
    });
    Array.prototype.forEach.call(scope.querySelectorAll("[data-i18n-ph]"), function (el) {
      var text = lookup(el.getAttribute("data-i18n-ph"));
      if (text !== undefined) {
        el.setAttribute("placeholder", text);
      }
    });
    Array.prototype.forEach.call(scope.querySelectorAll("[data-i18n-aria]"), function (el) {
      var text = lookup(el.getAttribute("data-i18n-aria"));
      if (text !== undefined) {
        el.setAttribute("aria-label", text);
      }
    });
    Array.prototype.forEach.call(scope.querySelectorAll("[data-i18n-title]"), function (el) {
      var text = lookup(el.getAttribute("data-i18n-title"));
      if (text !== undefined) {
        el.setAttribute("title", text);
      }
    });
  }

  /**
   * Applies the current language to the tree. Views call this after they
   * render dynamic rows; the shell calls it once per switch. It only replaces
   * text and attributes, never nodes.
   */
  var titleKey = "app.title";

  function apply(root) {
    applyNode(root);
    var doc = global.document;
    if (!doc) {
      return;
    }
    doc.documentElement.setAttribute("lang", state.lang === "zh" ? "zh-Hans" : "en");
    setTitle(titleKey);
  }

  /** Keeps document.title in sync; routers pass the current surface's key. */
  function setTitle(key) {
    titleKey = key || "app.title";
    if (!global.document) {
      return;
    }
    var appName = lookup("app.title") || "magnet";
    var surface = lookup(titleKey) || "";
    global.document.title = surface && surface !== appName ? appName + " · " + surface : appName;
  }

  function notify() {
    for (var i = 0; i < listeners.length; i++) {
      try {
        listeners[i](state.lang);
      } catch (err) {
        if (global.console && typeof global.console.error === "function") {
          global.console.error(err);
        }
      }
    }
  }

  /**
   * Persists and applies a preference. "auto" keeps following the client; the
   * resolved language is what the UI renders. Returns the resolved code.
   */
  function setLang(pref) {
    var next = pref === "auto" || SUPPORTED[pref] ? pref : "auto";
    state.pref = next;
    state.lang = resolve(next);

    try {
      if (next === "auto") {
        global.localStorage.removeItem(STORAGE_KEY);
      } else {
        global.localStorage.setItem(STORAGE_KEY, next);
      }
    } catch (err) {
      // Storage failure only costs persistence, not the switch.
    }

    apply();
    notify();
    return state.lang;
  }

  /** The stored preference: auto | zh | en. */
  function pref() {
    return state.pref;
  }

  /** The effective language: zh | en. */
  function lang() {
    return state.lang;
  }

  function onChange(fn) {
    if (typeof fn === "function") {
      listeners.push(fn);
    }
  }

  function init() {
    state.pref = readStoredPref();
    state.lang = resolve(state.pref);
    apply();
  }

  global.MagnetI18n = {
    init: init,
    t: t,
    plural: plural,
    num: num,
    date: date,
    monthDay: monthDay,
    time: time,
    dateTime: dateTime,
    apply: apply,
    setTitle: setTitle,
    setLang: setLang,
    pref: pref,
    lang: lang,
    onChange: onChange,
    normalizeLang: normalizeLang,
  };
})(window);
