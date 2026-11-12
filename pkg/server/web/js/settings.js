/*
 * settings.js — the settings root page, for every role.
 *
 * The page is deliberately short: account, this device, about, and — for an
 * administrator — one row that opens the console. Long lists live on their own
 * pages, so "About" keeps the same position no matter how many members exist.
 *
 * The language sheet here is the only language switch in the app. Device
 * preferences (language, appearance, reduced motion) are stored locally by
 * core.js/js/i18n.js; switching language rewrites text in place and never
 * rebuilds this screen, so an open sheet, focus and drafts survive.
 */
(function (global) {
  "use strict";

  var core = global.MagnetApp;
  if (!core) {
    return;
  }

  var i18n = global.MagnetI18n;
  var VIEW = "settings";
  var SECTION_ID = "view-settings";
  var MODE_RESTRICTED = "restricted";

  var state = {
    me: null,
    meState: "idle",
    stats: null,
    loaded: false,
  };

  var els = null;
  var sheet = null;

  /* Helpers -------------------------------------------------------------- */

  function t(key, vars) {
    return i18n ? i18n.t(key, vars) : key;
  }

  function esc(value) {
    return core.escapeHTML(value);
  }

  function api() {
    if (!global.MagnetAPI) {
      throw new Error("MagnetAPI is not loaded");
    }
    return global.MagnetAPI;
  }

  function isAdmin() {
    return Boolean(state.me && state.me.role === "admin");
  }

  function accessMode() {
    return state.me && state.me.accessMode ? String(state.me.accessMode) : "";
  }

  function restricted() {
    return accessMode() === MODE_RESTRICTED;
  }

  /* Template ------------------------------------------------------------- */

  function staticText(key) {
    return esc(t(key));
  }

  var TEMPLATE =
    '<div class="screen" data-role="screen">' +
    core.pageHeadHTML({ titleKey: "nav.settings" }) +
    '<div data-slot="error"></div>' +
    // Account — who you are, and whether this account may use the bot.
    '<section class="group" data-group="account">' +
    '<div class="group-head">' +
    '<h3 class="group-title" data-i18n="set.account">' +
    staticText("set.account") +
    "</h3>" +
    "</div>" +
    '<div class="row">' +
    '<div class="row-main">' +
    '<span class="row-lead">' +
    '<span data-slot="ident-name"></span>' +
    '<span class="row-id num" data-slot="ident-id"></span>' +
    '<span class="name-note" data-slot="ident-unnamed" hidden></span>' +
    "</span>" +
    '<span class="row-meta" data-slot="ident-role"></span>' +
    "</div>" +
    "</div>" +
    '<div class="row">' +
    '<div class="row-main">' +
    '<span class="row-lead" data-i18n="set.accessState">' +
    staticText("set.accessState") +
    "</span>" +
    "</div>" +
    '<span class="row-value" data-slot="access-state"></span>' +
    "</div>" +
    // The console is a capability of this account, so its entry sits here and
    // is absent for every other role.
    '<button type="button" class="row is-link" data-role="admin" hidden>' +
    '<span class="row-main">' +
    '<span class="row-lead" data-i18n="set.manage">' +
    staticText("set.manage") +
    "</span>" +
    '<span class="row-meta" data-i18n="set.manageSub">' +
    staticText("set.manageSub") +
    "</span>" +
    "</span>" +
    core.icon("chevron") +
    "</button>" +
    "</section>" +
    // This device — device-local preferences; language lives only here.
    '<section class="group" data-group="device">' +
    '<div class="group-head">' +
    '<h3 class="group-title" data-i18n="set.device">' +
    staticText("set.device") +
    "</h3>" +
    "</div>" +
    '<button type="button" class="row is-link" data-role="language">' +
    '<span class="row-main">' +
    '<span class="row-lead" data-i18n="set.language">' +
    staticText("set.language") +
    "</span>" +
    "</span>" +
    '<span class="row-value"><span data-slot="language-value"></span>' +
    core.icon("chevron") +
    "</span>" +
    "</button>" +
    '<button type="button" class="row is-link" data-role="appearance">' +
    '<span class="row-main">' +
    '<span class="row-lead" data-i18n="set.appearance">' +
    staticText("set.appearance") +
    "</span>" +
    "</span>" +
    '<span class="row-value"><span data-slot="appearance-value"></span>' +
    core.icon("chevron") +
    "</span>" +
    "</button>" +
    '<div class="row">' +
    '<div class="row-main">' +
    '<span class="row-lead" data-i18n="set.reduceMotion">' +
    staticText("set.reduceMotion") +
    "</span>" +
    '<span class="row-meta" data-i18n="set.reduceMotionHint">' +
    staticText("set.reduceMotionHint") +
    "</span>" +
    "</div>" +
    '<label class="switch"><input type="checkbox" data-role="reduce-motion" data-i18n-aria="aria.reduceMotion" aria-label="' +
    esc(t("aria.reduceMotion")) +
    '" /><span class="switch-track"></span></label>' +
    "</div>" +
    '<p class="group-note" data-i18n="set.deviceNote">' +
    staticText("set.deviceNote") +
    "</p>" +
    "</section>" +
    // About — which build this is and how to reach the bot; deployment detail
    // lives in the administrator console, not here.
    '<section class="group" data-group="about">' +
    '<div class="group-head">' +
    '<h3 class="group-title" data-i18n="set.about">' +
    staticText("set.about") +
    "</h3>" +
    "</div>" +
    '<div class="row">' +
    '<div class="row-main">' +
    '<span class="row-lead" data-i18n="about.version">' +
    staticText("about.version") +
    "</span>" +
    "</div>" +
    '<span class="row-value num" data-slot="about-version"></span>' +
    "</div>" +
    '<button type="button" class="row is-link" data-role="help" hidden>' +
    '<span class="row-main">' +
    '<span class="row-lead" data-i18n="about.help">' +
    staticText("about.help") +
    "</span>" +
    "</span>" +
    '<span class="row-value">' +
    core.icon("external") +
    "</span>" +
    "</button>" +
    "</section>" +
    "</div>";

  /* Rendering ------------------------------------------------------------ */

  function inlineError(container, key, retry) {
    container.innerHTML = "";
    var wrap = document.createElement("div");
    wrap.className = "inline-error";
    wrap.setAttribute("role", "alert");

    var icon = document.createElement("span");
    icon.innerHTML = core.icon("alert", "icon-sm");

    var msg = document.createElement("div");
    msg.className = "msg";
    var text = document.createElement("span");
    text.textContent = t(key);
    msg.appendChild(text);

    wrap.appendChild(icon.firstChild || icon);
    wrap.appendChild(msg);

    if (typeof retry === "function") {
      var button = document.createElement("button");
      button.type = "button";
      button.className = "btn-ghost btn-sm";
      button.textContent = t("common.retry");
      button.addEventListener("click", retry);
      wrap.appendChild(button);
    }

    container.appendChild(wrap);
  }

  function renderIdentity() {
    var me = state.me;
    if (!me) {
      els.identName.textContent = state.meState === "error" ? "—" : "";
      els.identId.textContent = "";
      els.identId.hidden = true;
      els.identUnnamed.hidden = true;
      els.identRole.textContent = "";
      return;
    }

    var ident = core.identity(me);
    els.identName.textContent = ident.name;
    els.identId.hidden = ident.unnamed;
    els.identId.textContent = ident.unnamed ? "" : "· " + ident.idLabel;
    els.identUnnamed.hidden = !ident.unnamed;
    els.identUnnamed.textContent = ident.unnamed ? t("identity.unnamed") : "";
    // The lead already carries the remark when there is one (the shared
    // identity rule), so the meta line holds the role and nothing else.
    els.identRole.textContent = t(me.role === "admin" ? "role.admin" : "role.user");
  }

  function renderAccess() {
    var mode = accessMode();
    if (!mode) {
      els.accessState.textContent = "—";
      return;
    }
    els.accessState.textContent = restricted() ? t("set.accessStateRestricted") : t("set.accessStateOpen");
  }

  function renderDevice() {
    var pref = i18n ? i18n.pref() : "auto";
    els.languageValue.textContent =
      pref === "zh" ? t("lang.zh") : pref === "en" ? t("lang.en") : t("lang.auto");

    var appearance = core.appearancePref();
    els.appearanceValue.textContent =
      appearance === "light"
        ? t("appearance.light")
        : appearance === "dark"
          ? t("appearance.dark")
          : t("appearance.auto");

    els.reduceMotion.checked = core.reduceMotion();
  }

  function renderAbout() {
    var me = state.me || {};
    var version = typeof me.version === "string" ? me.version.trim() : "";
    els.aboutVersion.textContent = version || "\u2014";

    var username = state.stats && state.stats.botUsername ? String(state.stats.botUsername) : "";
    els.help.hidden = username === "";
  }

  function renderError() {
    els.error.innerHTML = "";
    if (state.meState === "error") {
      inlineError(els.error, "set.profileFailed", function () {
        return load(true);
      });
    }
  }

  function renderAdminEntry() {
    els.adminRow.hidden = !isAdmin();
  }

  function renderAll() {
    if (!els) {
      return;
    }
    renderIdentity();
    renderAccess();
    renderDevice();
    renderAbout();
    renderError();
    renderAdminEntry();
    sheetLabels();
  }

  /* Sheets --------------------------------------------------------------- */

  function closeSheet() {
    if (!sheet) {
      return;
    }
    if (sheet.el.parentNode) {
      sheet.el.parentNode.removeChild(sheet.el);
    }
    if (sheet.scrim.parentNode) {
      sheet.scrim.parentNode.removeChild(sheet.scrim);
    }
    var last = sheet.lastFocus;
    sheet = null;
    if (last && typeof last.focus === "function") {
      last.focus();
    }
  }

  function registerPair(el, key, vars) {
    if (!sheet) {
      return el;
    }
    sheet.pairs.push({ el: el, key: key, vars: vars || null });
    el.textContent = t(key, vars);
    return el;
  }

  function sheetLabels() {
    if (!sheet) {
      return;
    }
    sheet.title.textContent = t(sheet.titleKey);
    sheet.kicker.hidden = !sheet.kickerKey;
    sheet.kicker.textContent = sheet.kickerKey ? t(sheet.kickerKey) : "";
    sheet.close.setAttribute("aria-label", t("aria.close"));
    sheet.pairs.forEach(function (pair) {
      pair.el.textContent = t(pair.key, pair.vars);
    });
    sheet.options.forEach(function (option) {
      var on = option.value === sheet.current;
      option.button.classList.toggle("is-on", on);
      option.button.setAttribute("aria-pressed", on ? "true" : "false");
    });
  }

  function openChoiceSheet(config) {
    closeSheet();

    var scrim = document.createElement("div");
    scrim.className = "scrim";

    var el = document.createElement("section");
    el.className = "sheet";
    el.setAttribute("role", "dialog");
    el.setAttribute("aria-modal", "true");
    el.setAttribute("aria-labelledby", "settings-sheet-title");
    el.innerHTML =
      '<div class="sheet-grab" aria-hidden="true"></div>' +
      '<div class="sheet-head"><div>' +
      '<h3 class="sheet-title" id="settings-sheet-title" data-role="title"></h3>' +
      '<p class="sheet-kicker" data-role="kicker"></p>' +
      "</div>" +
      '<button type="button" class="icon-btn" data-role="close">' +
      core.icon("close") +
      "</button></div>" +
      '<div class="sheet-body" data-role="body"></div>';

    sheet = {
      el: el,
      scrim: scrim,
      title: el.querySelector('[data-role="title"]'),
      kicker: el.querySelector('[data-role="kicker"]'),
      close: el.querySelector('[data-role="close"]'),
      pairs: [],
      options: [],
      titleKey: config.titleKey,
      kickerKey: config.kickerKey || "",
      current: config.current,
      lastFocus: document.activeElement,
    };

    var body = el.querySelector('[data-role="body"]');
    config.options.forEach(function (spec) {
      var button = document.createElement("button");
      button.type = "button";
      button.className = "choice";
      button.setAttribute("data-value", spec.value);

      var text = document.createElement("span");
      var label = document.createElement("span");
      label.className = "choice-label";
      registerPair(label, spec.labelKey);
      text.appendChild(label);

      if (spec.hintKey) {
        var hint = document.createElement("span");
        hint.className = "choice-hint";
        registerPair(hint, spec.hintKey);
        text.appendChild(hint);
      }

      button.appendChild(text);
      button.insertAdjacentHTML("beforeend", core.icon("check", "choice-mark"));
      button.addEventListener("click", function () {
        config.onPick(spec.value);
      });

      body.appendChild(button);
      sheet.options.push({ button: button, value: spec.value });
    });

    document.body.appendChild(scrim);
    document.body.appendChild(el);

    sheet.close.addEventListener("click", closeSheet);
    scrim.addEventListener("click", closeSheet);

    sheetLabels();

    var current = sheet.options.filter(function (option) {
      return option.value === sheet.current;
    })[0];
    var focusTarget = current ? current.button : sheet.close;
    focusTarget.focus();
  }

  function pickLanguage(value) {
    if (sheet) {
      // Update the mark before onChange renders, so the sheet never shows the
      // old choice between the switch and the close.
      sheet.current = value;
    }
    if (i18n) {
      i18n.setLang(value);
    }
    closeSheet();
    renderDevice();
  }

  function pickAppearance(value) {
    core.setAppearance(value);
    closeSheet();
    renderDevice();
  }

  function openLanguageSheet() {
    openChoiceSheet({
      titleKey: "set.language",
      kickerKey: "lang.sub",
      current: i18n ? i18n.pref() : "auto",
      options: [
        { value: "auto", labelKey: "lang.auto", hintKey: "lang.autoHint" },
        { value: "zh", labelKey: "lang.zh" },
        { value: "en", labelKey: "lang.en" },
      ],
      onPick: pickLanguage,
    });
  }

  function openAppearanceSheet() {
    openChoiceSheet({
      titleKey: "set.appearance",
      kickerKey: "appearance.sub",
      current: core.appearancePref(),
      options: [
        { value: "auto", labelKey: "appearance.auto", hintKey: "appearance.autoHint" },
        { value: "light", labelKey: "appearance.light" },
        { value: "dark", labelKey: "appearance.dark" },
      ],
      onPick: pickAppearance,
    });
  }

  function onKeydown(event) {
    if (!sheet) {
      return;
    }
    var key = event.key || "";
    if (key === "Escape" || key === "Esc") {
      event.preventDefault();
      closeSheet();
    }
  }

  /* Data ----------------------------------------------------------------- */

  function load(force) {
    var meTask = core
      .me(Boolean(force))
      .then(function (me) {
        state.me = me;
        state.meState = "ready";
      })
      .catch(function () {
        state.meState = "error";
      });

    var statsTask = api()
      .statistics()
      .then(function (payload) {
        state.stats = payload && typeof payload === "object" ? payload : null;
      })
      .catch(function () {
        // The help link is the only consumer; omit it when statistics fail.
      });

    renderAll();

    return Promise.all([meTask, statsTask])
      .then(function () {
        state.loaded = true;
        renderAll();
      });
  }

  function reload() {
    return load(true);
  }

  /* Wiring --------------------------------------------------------------- */

  function build(section) {
    section.innerHTML = TEMPLATE;
    els = {
      screen: section.querySelector('[data-role="screen"]'),
      error: section.querySelector('[data-slot="error"]'),
      identName: section.querySelector('[data-slot="ident-name"]'),
      identId: section.querySelector('[data-slot="ident-id"]'),
      identUnnamed: section.querySelector('[data-slot="ident-unnamed"]'),
      identRole: section.querySelector('[data-slot="ident-role"]'),
      accessState: section.querySelector('[data-slot="access-state"]'),
      languageValue: section.querySelector('[data-slot="language-value"]'),
      appearanceValue: section.querySelector('[data-slot="appearance-value"]'),
      reduceMotion: section.querySelector('[data-role="reduce-motion"]'),
      aboutVersion: section.querySelector('[data-slot="about-version"]'),
      help: section.querySelector('[data-role="help"]'),
      adminRow: section.querySelector('[data-role="admin"]'),
    };
  }

  function bind() {

    els.screen.querySelector('[data-role="language"]').addEventListener("click", openLanguageSheet);
    els.screen.querySelector('[data-role="appearance"]').addEventListener("click", openAppearanceSheet);
    els.screen.querySelector('[data-role="admin"]').addEventListener("click", function () {
      core.go("admin");
    });

    els.reduceMotion.addEventListener("change", function () {
      core.setReduceMotion(els.reduceMotion.checked);
    });

    els.help.addEventListener("click", function () {
      var username = state.stats && state.stats.botUsername ? String(state.stats.botUsername) : "";
      if (username) {
        core.openTelegramLink("https://t.me/" + username);
      }
    });

    document.addEventListener("keydown", onKeydown);
  }

  function onLang() {
    renderAll();
  }

  function mount() {
    var section = document.getElementById(SECTION_ID);
    if (!section) {
      return;
    }
    if (!els) {
      build(section);
      bind();
    }

    if (!state.loaded || state.meState === "error" || core.state.isStale(VIEW)) {
      return load(false);
    }

    renderAll();
    return Promise.resolve();
  }

  core.registerView(VIEW, { mount: mount, onLang: onLang, onResume: reload });
})(window);
