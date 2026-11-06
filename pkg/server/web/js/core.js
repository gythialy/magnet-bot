/*
 * core.js — the Mini App shell.
 *
 * Responsibilities, and nothing else:
 *   - Telegram bootstrap (ready/expand, themeParams -> CSS variables)
 *   - appearance (auto/light/dark) and reduced motion, both device-local
 *   - the "not opened from the bot" gate
 *   - hash routing across the four tabs and the two sub-pages
 *   - the language switch wiring (the dictionaries live in js/locales)
 *   - the transport helper every /api call goes through
 *   - small shared utilities (icon, toast, sheet binding, escapeHTML, debounce)
 *
 * Page behaviour belongs to js/*.js; they register themselves through
 * MagnetApp.registerView, so this file never needs to know what a view renders.
 *
 * The single entry point is window.MagnetApp.
 *
 * No build step, no third-party code, no external requests.
 */
(function (global) {
  "use strict";

  var DEFAULT_ROUTE = "today";
  // Tab roots. Sub-pages (members, admin) are routes too, but they keep the
  // parent tab highlighted and never own a tab of their own.
  var TABS = ["today", "library", "records", "settings"];
  var SUB_PAGES = { members: "library", admin: "settings" };
  var ROUTES = TABS.concat(Object.keys(SUB_PAGES));
  var ROUTE_TITLES = {
    today: "today.title",
    library: "lib.title",
    records: "records.title",
    settings: "nav.settings",
    members: "pick.title",
    admin: "set.manage",
  };

  // Telegram themeParams key -> CSS custom property consumed by css/base.css.
  var THEME_VARIABLES = {
    bg_color: "--tg-bg",
    text_color: "--tg-text",
    hint_color: "--tg-hint",
    button_color: "--tg-button",
    button_text_color: "--tg-button-text",
    secondary_bg_color: "--tg-secondary-bg",
    link_color: "--tg-link",
  };

  var LIGHT_PALETTE = {
    bg_color: "#ffffff",
    text_color: "#09090b",
    hint_color: "#71717a",
    button_color: "#18181b",
    button_text_color: "#fafafa",
    secondary_bg_color: "#f4f4f5",
    link_color: "#1668a8",
  };

  // Telegram always sends every key in themeParams, but a dark client that
  // omits one must not leave a white surface behind.
  var DARK_FALLBACKS = {
    bg_color: "#17212b",
    text_color: "#f5f5f5",
    hint_color: "#7d8b99",
    button_color: "#5288c1",
    button_text_color: "#ffffff",
    secondary_bg_color: "#232e3c",
    link_color: "#6ab3f3",
  };

  var STORAGE_APPEARANCE = "magnet.appearance";
  var STORAGE_MOTION = "magnet.reduceMotion";
  var TOAST_TIMEOUT_MS = 3000;

  var views = new Map();

  // Staleness is a counter, not a boolean: a view whose mount is asynchronous
  // checks isStale() after an await, so the flag must survive until that check
  // ran. mountView only clears the generation it started with, which keeps an
  // invalidation that lands during a mount from being swallowed.
  var staleCounts = new Map();

  function generationOf(name) {
    return staleCounts.get(name) || 0;
  }

  function invalidateView(name) {
    staleCounts.set(name, generationOf(name) + 1);
  }

  var state = {
    initData: "",
    route: DEFAULT_ROUTE,
    // The account an administrator is inspecting right now. In memory only:
    // reopening or refreshing the Mini App returns to the caller's own list.
    viewingUser: null,
    /** Mark a view (or every view when called with no name) as needing a remount. */
    invalidate: function (name) {
      if (name) {
        invalidateView(name);
      } else {
        ROUTES.forEach(invalidateView);
      }
      return state;
    },
    /** True while the named view still owes a render. */
    isStale: function (name) {
      return generationOf(name) > 0;
    },
    /** Internal: clears exactly the generation a mount started with. */
    clearStale: function (name, generation) {
      if (generationOf(name) === generation) {
        staleCounts.delete(name);
      }
    },
    generationOf: generationOf,
  };

  function telegram() {
    return global.Telegram && global.Telegram.WebApp ? global.Telegram.WebApp : null;
  }

  function byId(id) {
    return document.getElementById(id);
  }

  function i18n() {
    return global.MagnetI18n;
  }

  function t(key, vars) {
    var layer = i18n();
    return layer ? layer.t(key, vars) : key;
  }

  /* Theme ---------------------------------------------------------------- */

  // Telegram's button colour is chosen to sit under white text, not on top of
  // the background: as link/tab text it lands around 4.1:1 on a light theme.
  // The derived token keeps the client's hue while meeting the 4.5:1 floor.
  function parseHex(value) {
    var raw = String(value || "").trim().replace("#", "");
    if (raw.length === 3) {
      raw = raw[0] + raw[0] + raw[1] + raw[1] + raw[2] + raw[2];
    }
    if (!/^[0-9a-fA-F]{6}$/.test(raw)) {
      return null;
    }
    return {
      r: parseInt(raw.slice(0, 2), 16),
      g: parseInt(raw.slice(2, 4), 16),
      b: parseInt(raw.slice(4, 6), 16),
    };
  }

  function relativeLuminance(rgb) {
    var channels = [rgb.r, rgb.g, rgb.b].map(function (value) {
      var c = value / 255;
      return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
    });
    return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
  }

  function contrastRatio(a, b) {
    var la = relativeLuminance(a);
    var lb = relativeLuminance(b);
    var lighter = Math.max(la, lb);
    var darker = Math.min(la, lb);
    return (lighter + 0.05) / (darker + 0.05);
  }

  function readableAccent(accent, background) {
    var fg = parseHex(accent);
    var bg = parseHex(background) || { r: 255, g: 255, b: 255 };
    if (!fg) {
      return "";
    }

    // Mix towards black or white (whichever the background is not) until the
    // pair clears 4.5:1, keeping the hue the client chose.
    var towardDark = relativeLuminance(bg) > 0.5;
    var tinted = fg;
    for (var step = 0; step < 20 && contrastRatio(tinted, bg) < 4.5; step++) {
      tinted = {
        r: towardDark ? tinted.r * 0.92 : tinted.r + (255 - tinted.r) * 0.08,
        g: towardDark ? tinted.g * 0.92 : tinted.g + (255 - tinted.g) * 0.08,
        b: towardDark ? tinted.b * 0.92 : tinted.b + (255 - tinted.b) * 0.08,
      };
    }

    function hex(value) {
      var part = Math.max(0, Math.min(255, Math.round(value))).toString(16);
      return part.length === 1 ? "0" + part : part;
    }

    return "#" + hex(tinted.r) + hex(tinted.g) + hex(tinted.b);
  }

  function themeFallbackAccent(isDark) {
    return isDark ? DARK_FALLBACKS.link_color : LIGHT_PALETTE.link_color;
  }

  function readPreference(key) {
    try {
      return global.localStorage.getItem(key) || "";
    } catch (err) {
      return "";
    }
  }

  function writePreference(key, value) {
    try {
      if (value) {
        global.localStorage.setItem(key, value);
      } else {
        global.localStorage.removeItem(key);
      }
    } catch (err) {
      // Storage failure only costs persistence, not the current render.
    }
  }

  /** The stored appearance preference: auto | light | dark. */
  function appearancePref() {
    var stored = readPreference(STORAGE_APPEARANCE);
    return stored === "light" || stored === "dark" ? stored : "auto";
  }

  function clientIsDark() {
    var webApp = telegram();
    return Boolean(webApp && webApp.colorScheme === "dark");
  }

  function resolvedAppearance() {
    var pref = appearancePref();
    return pref === "auto" ? (clientIsDark() ? "dark" : "light") : pref;
  }

  function applyTheme() {
    var webApp = telegram();
    var params = webApp ? webApp.themeParams || {} : {};
    var isDark = resolvedAppearance() === "dark";
    var forced = appearancePref() !== "auto";
    var palette = forced ? (isDark ? DARK_FALLBACKS : LIGHT_PALETTE) : params;
    var fallbacks = isDark ? DARK_FALLBACKS : LIGHT_PALETTE;
    var root = document.documentElement;

    Object.keys(THEME_VARIABLES).forEach(function (key) {
      var value = palette[key] || (forced ? fallbacks[key] : "") || (isDark ? DARK_FALLBACKS[key] : "");
      if (value) {
        root.style.setProperty(THEME_VARIABLES[key], value);
      }
    });

    // Link and active-tab text use the derived accent; buttons keep the raw
    // Telegram colour because their label is provided white-on-colour.
    var accentSource =
      palette.link_color || palette.button_color || themeFallbackAccent(isDark);
    var background = palette.bg_color || (isDark ? DARK_FALLBACKS.bg_color : LIGHT_PALETTE.bg_color);
    var accent = readableAccent(accentSource, background);
    if (accent) {
      root.style.setProperty("--app-accent-text", accent);
    }

    root.setAttribute("data-theme", isDark ? "dark" : "light");
    var meta = document.querySelector("meta[name='theme-color']");
    if (meta) {
      meta.setAttribute("content", isDark ? DARK_FALLBACKS.bg_color : LIGHT_PALETTE.bg_color);
    }
  }

  /** Stores and applies one appearance choice (auto|light|dark). */
  function setAppearance(pref) {
    writePreference(STORAGE_APPEARANCE, pref === "auto" ? "" : pref);
    applyTheme();
    return resolvedAppearance();
  }

  function reduceMotionEnabled() {
    return readPreference(STORAGE_MOTION) === "1";
  }

  function applyReduceMotion() {
    document.documentElement.setAttribute(
      "data-motion",
      reduceMotionEnabled() ? "reduce" : "auto"
    );
  }

  function setReduceMotion(enabled) {
    writePreference(STORAGE_MOTION, enabled ? "1" : "");
    applyReduceMotion();
    return reduceMotionEnabled();
  }

  /* Icons ---------------------------------------------------------------- */

  // One stroked family, drawn here so every view uses the same geometry and
  // inherits currentColor. Font glyphs and emoji are banned by the design
  // system: they render differently on every platform and carry their own
  // colour.
  var ICONS = {
    search: '<circle cx="11" cy="11" r="6.5"/><path d="M16 16l4.5 4.5"/>',
    refresh: '<path d="M20 12a8 8 0 1 1-2.6-5.9"/><path d="M20 4v5h-5"/>',
    close: '<path d="M6 6l12 12"/><path d="M18 6L6 18"/>',
    list: '<path d="M5 7h14"/><path d="M5 12h14"/><path d="M5 17h9"/>',
    hash: '<path d="M9 4L7 20"/><path d="M17 4l-2 16"/><path d="M4 9h16"/><path d="M3 15h16"/>',
    bell: '<path d="M6 9a6 6 0 1 1 12 0c0 4 1.5 5.5 2 6H4c.5-.5 2-2 2-6z"/><path d="M10 19a2 2 0 0 0 4 0"/>',
    clock: '<circle cx="12" cy="12" r="8"/><path d="M12 8v4.5l3 1.8"/>',
    sliders:
      '<path d="M4 8h9"/><path d="M17 8h3"/><circle cx="15" cy="8" r="2"/><path d="M4 16h3"/><path d="M11 16h9"/><circle cx="9" cy="16" r="2"/>',
    pdf: '<path d="M6 3h8l4 4v14H6z"/><path d="M14 3v4h4"/><path d="M9 13h6"/><path d="M9 17h4"/>',
    png: '<rect x="4" y="5" width="16" height="14" rx="1.5"/><path d="M4 16l4.5-4.5L13 16"/><path d="M13 16l3-3 4 4"/><circle cx="9.2" cy="9.2" r="1.2"/>',
    external: '<path d="M14 4h6v6"/><path d="M20 4l-8 8"/><path d="M18 14v6H4V6h6"/>',
    tender: '<path d="M12 3l2.6 5.6 6.1.8-4.5 4.2 1.2 6-5.4-3-5.4 3 1.2-6L3.3 9.4l6.1-.8z"/>',
    edit: '<path d="M4 20h4L19 9l-4-4L4 16v4z"/><path d="M14 6l4 4"/>',
    trash: '<path d="M4 7h16"/><path d="M9 7V4h6v3"/><path d="M6 7l1 13h10l1-13"/><path d="M10 11v6"/><path d="M14 11v6"/>',
    plus: '<path d="M12 5v14"/><path d="M5 12h14"/>',
    alert: '<path d="M12 4.6l8.4 14.8H3.6z"/><path d="M12 9.8v4.6"/><path d="M12 17.2v.2"/>',
    check: '<path d="M5 12.5l4.5 4.5L19 7"/>',
    archive: '<path d="M4 8h16"/><path d="M5.5 8v11.5h13V8"/><path d="M12 11v4"/><path d="M9.6 12.8L12 15.2l2.4-2.4"/>',
    undo: '<path d="M20 12a8 8 0 1 1-2.6-5.9"/><path d="M20 12V7h-5"/>',
    lock: '<rect x="5" y="10.5" width="14" height="9.5" rx="2"/><path d="M8.5 10.5V7.5a3.5 3.5 0 0 1 7 0v3"/>',
    users:
      '<circle cx="9" cy="8" r="3"/><path d="M3.5 19.5c0-3 2.5-5 5.5-5s5.5 2 5.5 5"/><path d="M16.5 5.4a3 3 0 0 1 0 5.2"/><path d="M17.5 14.7c1.9.5 3 2.1 3 4.8"/>',
    eye: '<path d="M2.6 12S6.2 5.6 12 5.6 21.4 12 21.4 12 17.8 18.4 12 18.4 2.6 12 2.6 12z"/><circle cx="12" cy="12" r="2.6"/>',
    shield: '<path d="M12 3.2l7 2.9v5.4c0 4-3 6.8-7 9.3-4-2.5-7-5.3-7-9.3V6.1z"/>',
    chevron: '<path d="M9.5 6l6 6-6 6"/>',
  };

  function icon(name, extraClass) {
    var body = ICONS[name];
    if (!body) {
      return "";
    }
    return (
      '<svg class="icon ' +
      (extraClass || "") +
      '" viewBox="0 0 24 24" aria-hidden="true" focusable="false" ' +
      'fill="none" stroke="currentColor" stroke-width="1.6" ' +
      'stroke-linecap="round" stroke-linejoin="round">' +
      body +
      "</svg>"
    );
  }

  /* Shared markup -------------------------------------------------------- */

  /** The root-page top bar: brand and refresh. */
  function topbarHTML() {
    return (
      '<header class="topbar">' +
      '<div class="brand"><span class="brand-mark"></span>magnet</div>' +
      '<div class="topbar-actions">' +
      '<button type="button" class="icon-btn" data-role="refresh" aria-label="' +
      escapeHTML(t("aria.refresh")) +
      '" data-i18n-aria="aria.refresh">' +
      icon("refresh") +
      "</button>" +
      "</div>" +
      "</header>"
    );
  }

  /** A sub-page top bar: back, title, spacer keeps the title centred. */
  function subTopbarHTML(titleKey) {
    return (
      '<header class="topbar is-sub">' +
      '<button type="button" class="icon-btn is-back" data-role="back" aria-label="' +
      escapeHTML(t("aria.back")) +
      '" data-i18n-aria="aria.back">' +
      icon("chevron") +
      "</button>" +
      '<span class="topbar-title" data-i18n="' +
      escapeHTML(titleKey) +
      '">' +
      escapeHTML(t(titleKey)) +
      "</span>" +
      '<span class="topbar-spacer" aria-hidden="true"></span>' +
      "</header>"
    );
  }

  /** Binds the refresh button inside one rendered section. */
  function bindRefresh(section, onRefresh) {
    var button = section.querySelector('[data-role="refresh"]');
    if (button) {
      button.addEventListener("click", function () {
        onRefresh();
      });
    }
  }

  /** Binds the back button inside a sub-page section. */
  function bindBack(section, onBack) {
    var button = section.querySelector('[data-role="back"]');
    if (button) {
      button.addEventListener("click", function () {
        onBack();
      });
    }
  }

  /* Gate ----------------------------------------------------------------- */

  function showGate() {
    var gate = byId("app-gate");
    var shell = byId("app-shell");
    if (shell) {
      shell.hidden = true;
    }
    if (gate) {
      gate.hidden = false;
    }
  }

  /* Routing -------------------------------------------------------------- */

  function normalizeRoute(hash) {
    return String(hash || "")
      .replace(/^#\/?/, "")
      .replace(/\/+$/, "");
  }

  // The tab that owns a route: sub-pages keep their parent highlighted.
  function parentTab(name) {
    return SUB_PAGES[name] || name;
  }

  function renderShell(name) {
    ROUTES.forEach(function (route) {
      var section = byId("view-" + route);
      if (section) {
        section.hidden = route !== name;
      }
    });

    var active = parentTab(name);
    var tabs = document.querySelectorAll("#tabbar [data-route]");
    Array.prototype.forEach.call(tabs, function (tab) {
      var tabRoute = normalizeRoute(tab.getAttribute("data-route") || tab.getAttribute("href"));
      tab.classList.toggle("is-active", tabRoute === active);
      if (tabRoute === active) {
        tab.setAttribute("aria-current", "page");
      } else {
        tab.removeAttribute("aria-current");
      }
    });

    var layer = i18n();
    if (layer) {
      layer.setTitle(ROUTE_TITLES[name] || "app.title");
    }
  }

  function mountView(name) {
    var view = views.get(name);
    if (!view || typeof view.mount !== "function") {
      // No script owns this view yet: the skeleton in index.html stays visible,
      // which is a truthful "nothing loaded" rather than a fake empty state.
      return;
    }

    // The flag is cleared after mount() finished, not before: a view decides
    // whether to reload by reading state.isStale(), and an async mount reads it
    // after an await. A throwing or rejecting mount keeps the flag so the next
    // visit retries instead of silently serving stale data.
    var generation = generationOf(name);
    try {
      var result = view.mount();
      if (result && typeof result.then === "function") {
        result.then(
          function () {
            state.clearStale(name, generation);
          },
          reportViewError
        );
      } else {
        state.clearStale(name, generation);
      }
    } catch (err) {
      reportViewError(err);
    }
  }

  function reportViewError(err) {
    if (global.console && typeof global.console.error === "function") {
      global.console.error(err);
    }
    toast(t("app.error"), "error");
  }

  /**
   * Resolve the current hash, normalise it, keep the tab bar in sync and mount
   * the matching view. Unknown or missing hashes fall back to #/today.
   */
  function route() {
    var requested = normalizeRoute(global.location.hash);
    var name = ROUTES.indexOf(requested) === -1 ? DEFAULT_ROUTE : requested;

    // Sub-pages are administrator surfaces; a non-admin typed hash must not
    // even mount the section (the view also re-checks through /api/me).
    state.route = name;

    // replaceState rather than assigning location.hash: assigning would append
    // a history entry per normalisation and re-enter this function.
    if (global.location.hash !== "#/" + name) {
      try {
        global.history.replaceState(null, "", "#/" + name);
      } catch (err) {
        // Some embedded webviews refuse history writes; routing still works.
        if (global.console && typeof global.console.error === "function") {
          global.console.error(err);
        }
      }
    }

    renderShell(name);
    mountView(name);

    return name;
  }

  /** Navigates without leaving the app; used by sub-pages and member picker. */
  function go(name) {
    var next = ROUTES.indexOf(name) === -1 ? DEFAULT_ROUTE : name;
    if (normalizeRoute(global.location.hash) === next) {
      route();
      return;
    }
    global.location.hash = "#/" + next;
  }

  function bindRouting() {
    global.addEventListener("hashchange", route);

    var tabs = document.querySelectorAll("#tabbar [data-route]");
    Array.prototype.forEach.call(tabs, function (tab) {
      tab.addEventListener("click", function () {
        // Let the browser update the hash; hashchange performs the render.
        if (normalizeRoute(tab.getAttribute("data-route")) === state.route) {
          route();
        }
      });
    });
  }

  /* Views ---------------------------------------------------------------- */

  /**
   * Register a view renderer: registerView("today", {mount: fn, onLang: fn}).
   * mount is called with no arguments whenever the route becomes active, and it
   * must be safe to call repeatedly. onLang, when present, re-renders only the
   * text the view owns — it must not rebuild the DOM.
   */
  function registerView(name, view) {
    if (ROUTES.indexOf(name) === -1) {
      throw new Error("MagnetApp: unknown view " + name);
    }
    if (!view || typeof view.mount !== "function") {
      throw new Error("MagnetApp: view " + name + " needs a mount function");
    }
    views.set(name, view);

    // A view script loaded after the initial route render still gets to draw.
    if (name === state.route) {
      mountView(name);
    }
  }

  /* Utilities ------------------------------------------------------------ */

  /**
   * toast("saved", "success") — transient status line. Passing
   * {actionLabel, onAction, timeout} adds the reversible-action button the
   * archive flow needs (the 5s undo window).
   */
  function toast(message, kind, options) {
    var container = byId("toasts") || document.body;
    var allowed = { info: true, success: true, error: true };
    var tone = allowed[kind] ? kind : "info";
    var opts = options || {};

    var el = document.createElement("div");
    el.className = "toast is-" + tone;
    el.setAttribute("role", "status");

    var text = document.createElement("span");
    text.textContent = String(message == null ? "" : message);
    el.appendChild(text);

    var timer = null;
    function dismiss() {
      if (timer !== null) {
        global.clearTimeout(timer);
      }
      if (el.parentNode) {
        el.parentNode.removeChild(el);
      }
    }

    if (opts.actionLabel) {
      var action = document.createElement("button");
      action.type = "button";
      action.className = "toast-action";
      action.textContent = String(opts.actionLabel);
      action.addEventListener("click", function () {
        dismiss();
        if (typeof opts.onAction === "function") {
          opts.onAction();
        }
      });
      el.appendChild(action);
    }

    container.appendChild(el);
    timer = global.setTimeout(dismiss, opts.timeout || TOAST_TIMEOUT_MS);
    return el;
  }

  // openLink is for ordinary web targets. A t.me link handed to openLink
  // opens the system browser first and only then bounces back into Telegram;
  // openTelegramLink keeps the whole trip inside the client.
  function openLink(url) {
    var target = String(url == null ? "" : url);
    if (!target) {
      return;
    }

    var webApp = telegram();
    if (webApp && typeof webApp.openLink === "function") {
      webApp.openLink(target);
      return;
    }

    global.open(target, "_blank", "noopener");
  }

  function openTelegramLink(url) {
    var target = String(url == null ? "" : url);
    if (!target) {
      return;
    }

    var webApp = telegram();
    if (webApp && typeof webApp.openTelegramLink === "function") {
      webApp.openTelegramLink(target);
      return;
    }

    openLink(target);
  }

  function escapeHTML(value) {
    var replacements = {
      "&": "&amp;",
      "<": "&lt;",
      ">": "&gt;",
      '"': "&quot;",
      "'": "&#39;",
    };
    return String(value == null ? "" : value).replace(/[&<>"']/g, function (ch) {
      return replacements[ch];
    });
  }

  /* Identity ------------------------------------------------------------- */

  /**
   * One display rule everywhere a person is listed: remark -> Telegram name ->
   * @username -> #id unnamed. The id always stays visible next to the name;
   * without any name the row says "unnamed" instead of pretending the id is a
   * name.
   */
  function identity(user) {
    var row = user || {};
    var remark = String(row.remark || "").trim();
    var name = String(row.name || "").trim();
    var username = String(row.username || "").trim().replace(/^@/, "");
    var label = remark || name || (username ? "@" + username : "");
    var unnamed = label === "";
    var idLabel = "#" + String(row.userId == null ? "" : row.userId);
    var initial = unnamed ? "#" : label.replace(/^@/, "").slice(0, 1).toUpperCase();

    return {
      name: unnamed ? idLabel : label,
      unnamed: unnamed,
      idLabel: idLabel,
      initial: initial,
      full: unnamed ? idLabel + " " + t("identity.unnamed") : label + " · " + idLabel,
    };
  }

  /**
   * The authenticated caller, fetched once per session. Views that only need
   * role/access mode share this instead of each issuing /api/me on mount;
   * force=true refetches after a role change.
   */
  var mePromise = null;

  function me(force) {
    if (!force) {
      if (mePromise) {
        return mePromise;
      }
      mePromise = request("/api/me");
    } else {
      mePromise = request("/api/me");
    }
    mePromise.catch(function () {
      mePromise = null;
    });
    return mePromise;
  }

  function debounce(fn, ms) {
    var wait = typeof ms === "number" ? ms : 250;
    var timer = null;

    return function debounced() {
      var args = arguments;
      var context = this;
      if (timer !== null) {
        global.clearTimeout(timer);
      }
      timer = global.setTimeout(function () {
        timer = null;
        fn.apply(context, args);
      }, wait);
    };
  }

  /* Transport ------------------------------------------------------------ */

  function requestError(response, message, code) {
    var err = new Error(message);
    err.status = response ? response.status : 0;
    err.code = code || "request_failed";
    return err;
  }

  // The API answers failures with {"error":{"code":"...","message":"..."}}.
  // Anything else (a proxy HTML page, an empty body) must still yield a typed
  // error rather than throwing a SyntaxError at the caller.
  function parseErrorBody(payload, response) {
    var detail = payload && payload.error ? payload.error : payload;
    var code = "http_" + response.status;
    var message = "Request failed (" + response.status + ")";

    if (detail && typeof detail === "object") {
      if (detail.code) {
        code = String(detail.code);
      }
      if (detail.message) {
        message = String(detail.message);
      }
      if (detail.reason) {
        // Guard refusals carry a machine-readable reason next to the code so
        // the client can translate it instead of showing fixed English.
        code = String(detail.reason);
      }
    }

    return requestError(response, message, code);
  }

  /**
   * request("/api/notices", {method: "GET", body: {...}})
   *
   * Every call carries the raw initData header the server authenticates; the
   * body is JSON-encoded unless it is already a string. Non-2xx responses throw
   * an Error carrying status and code, and a 401 is additionally surfaced as a
   * toast because the session cannot recover without reopening the Mini App.
   */
  function request(path, options) {
    if (!state.initData) {
      return Promise.reject(
        requestError(null, t("app.gateTitle"), "unauthenticated")
      );
    }

    var opts = options || {};
    var headers = {
      "X-Telegram-Init-Data": state.initData,
      "Content-Type": "application/json",
    };
    var init = {
      method: opts.method || "GET",
      headers: headers,
      credentials: "same-origin",
      cache: "no-store",
    };

    if (opts.body !== undefined && opts.body !== null) {
      init.body = typeof opts.body === "string" ? opts.body : JSON.stringify(opts.body);
    }

    return fetch(path, init).then(function (response) {
      if (response.status === 401) {
        toast(t("app.sessionExpired"), "error");
      }

      if (!response.ok) {
        return response
          .json()
          .then(function (payload) {
            throw parseErrorBody(payload, response);
          })
          .catch(function (err) {
            // A non-JSON error body rejects above with our typed error; anything
            // else (SyntaxError from response.json) becomes the generic error.
            if (err instanceof Error && err.status) {
              throw err;
            }
            throw parseErrorBody(null, response);
          });
      }

      if (response.status === 204) {
        return null;
      }

      var contentType = response.headers.get("Content-Type") || "";
      if (contentType.indexOf("application/json") === -1) {
        return response.text();
      }

      return response.json();
    });
  }

  /* Bootstrap ------------------------------------------------------------ */

  function init() {
    var webApp = telegram();
    var initData = webApp && typeof webApp.initData === "string" ? webApp.initData : "";

    state.initData = initData;
    state.viewingUser = null;

    if (webApp) {
      if (typeof webApp.ready === "function") {
        webApp.ready();
      }
      if (typeof webApp.expand === "function") {
        webApp.expand();
      }
      if (typeof webApp.onEvent === "function") {
        webApp.onEvent("themeChanged", applyTheme);
      }
    }

    applyTheme();
    applyReduceMotion();

    var layer = i18n();
    if (layer) {
      layer.init();
      layer.onChange(function () {
        layer.apply();
        views.forEach(function (view, name) {
          if (name === state.route && typeof view.onLang === "function") {
            try {
              view.onLang();
            } catch (err) {
              reportViewError(err);
            }
          }
        });
      });
    }

    // ready() must be called even when this page ends up showing the gate: the
    // client keeps its own loading overlay up until the page reports ready, so
    // the gate would otherwise stay hidden behind a spinner forever.
    if (!initData) {
      // Without credentials every request would be rejected, so stop before
      // binding routes or touching the network.
      showGate();
      if (layer) {
        layer.apply();
      }
      return;
    }

    bindRouting();

    if (!global.location.hash) {
      try {
        global.history.replaceState(null, "", "#/" + DEFAULT_ROUTE);
      } catch (err) {
        if (global.console && typeof global.console.error === "function") {
          global.console.error(err);
        }
      }
    }

    route();
  }

  document.addEventListener("DOMContentLoaded", init);

  global.MagnetApp = {
    init: init,
    route: route,
    go: go,
    toast: toast,
    openLink: openLink,
    openTelegramLink: openTelegramLink,
    icon: icon,
    escapeHTML: escapeHTML,
    debounce: debounce,
    request: request,
    me: me,
    identity: identity,
    registerView: registerView,
    topbarHTML: topbarHTML,
    subTopbarHTML: subTopbarHTML,
    bindRefresh: bindRefresh,
    bindBack: bindBack,
    setAppearance: setAppearance,
    appearance: resolvedAppearance,
    appearancePref: appearancePref,
    setReduceMotion: setReduceMotion,
    reduceMotion: reduceMotionEnabled,
    state: state,
  };
})(window);
