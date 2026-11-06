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
    status: null,
    statusState: "idle",
    statusError: null,
    stats: null,
    loaded: false,
  };

  var els = null;
  var sheet = null;

  /* Helpers -------------------------------------------------------------- */

  function t(key, vars) {
    return i18n ? i18n.t(key, vars) : key;
  }

  function num(value) {
    return i18n ? i18n.num(value) : String(value);
  }

  function dateTime(value) {
    if (!i18n) {
      return String(value || "");
    }
    var day = i18n.date(value);
    var clock = typeof i18n.time === "function" ? i18n.time(value) : "";
    return [day, clock].filter(Boolean).join(" ");
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
    if (state.status && state.status.accessMode) {
      return String(state.status.accessMode);
    }
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
    core.topbarHTML() +
    '<div class="view-head">' +
    '<h2 class="view-title" data-i18n="nav.settings">' +
    staticText("nav.settings") +
    "</h2>" +
    '<p class="view-sub" data-i18n="set.sub">' +
    staticText("set.sub") +
    "</p>" +
    "</div>" +
    '<div data-slot="error"></div>' +
    // Your account — identity comes from Telegram and is read only here.
    '<section class="group" data-group="account">' +
    '<div class="group-head">' +
    '<h3 class="group-title" data-i18n="set.account">' +
    staticText("set.account") +
    "</h3>" +
    '<span class="aside-note" data-i18n="set.accountNote">' +
    staticText("set.accountNote") +
    "</span>" +
    "</div>" +
    '<div class="row">' +
    '<div class="row-main">' +
    '<span class="row-lead">' +
    '<span data-slot="ident-name"></span>' +
    '<span class="row-id num" data-slot="ident-id"></span>' +
    '<span class="name-note" data-slot="ident-unnamed" hidden></span>' +
    '<span class="name-note" data-slot="ident-you" data-i18n="set.you"> ' +
    staticText("set.you") +
    "</span>" +
    "</span>" +
    '<span class="row-meta">' +
    '<span data-slot="ident-role"></span>' +
    '<span class="sep" data-slot="ident-sep" hidden>·</span>' +
    '<span data-slot="ident-remark"></span>' +
    "</span>" +
    "</div>" +
    "</div>" +
    '<div class="row">' +
    '<div class="row-main">' +
    '<span class="row-lead" data-i18n="set.accessState">' +
    staticText("set.accessState") +
    "</span>" +
    '<span class="row-meta" data-slot="access-state"></span>' +
    "</div>" +
    "</div>" +
    "</section>" +
    // This device — device-local preferences; language lives only here.
    '<section class="group" data-group="device">' +
    '<div class="group-head">' +
    '<h3 class="group-title" data-i18n="set.device">' +
    staticText("set.device") +
    "</h3>" +
    '<span class="aside-note" data-i18n="set.deviceNote">' +
    staticText("set.deviceNote") +
    "</span>" +
    "</div>" +
    '<button type="button" class="row is-link" data-role="language">' +
    '<span class="row-lead" data-i18n="set.language">' +
    staticText("set.language") +
    "</span>" +
    '<span class="row-value"><span data-slot="language-value"></span>' +
    core.icon("chevron") +
    "</span>" +
    "</button>" +
    '<button type="button" class="row is-link" data-role="appearance">' +
    '<span class="row-lead" data-i18n="set.appearance">' +
    staticText("set.appearance") +
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
    "</section>" +
    // About — fixed content, always before the administrator block.
    '<section class="group" data-group="about">' +
    '<div class="group-head">' +
    '<h3 class="group-title" data-i18n="set.about">' +
    staticText("set.about") +
    "</h3>" +
    '<span class="aside-note num" data-slot="version" hidden></span>' +
    "</div>" +
    '<div data-slot="about-error"></div>' +
    '<div class="status-row" data-row="build" hidden>' +
    '<span class="label" data-i18n="about.build">' +
    staticText("about.build") +
    "</span>" +
    '<span class="status-val num" data-slot="about-build"></span>' +
    "</div>" +
    '<div class="status-row" data-row="mode">' +
    '<span class="label" data-i18n="about.mode">' +
    staticText("about.mode") +
    "</span>" +
    '<span class="status-val" data-slot="about-mode"></span>' +
    "</div>" +
    '<div class="status-row" data-row="webhook" hidden>' +
    '<span class="label" data-i18n="about.webhook">' +
    staticText("about.webhook") +
    "</span>" +
    '<span class="status-val" data-slot="about-webhook"></span>' +
    "</div>" +
    '<div class="status-row" data-row="users" hidden>' +
    '<span class="label" data-i18n="about.users">' +
    staticText("about.users") +
    "</span>" +
    '<span class="status-val num" data-slot="about-users"></span>' +
    "</div>" +
    '<button type="button" class="link-row" data-role="help" hidden>' +
    "<span data-i18n=\"about.help\">" +
    staticText("about.help") +
    "</span>" +
    core.icon("external") +
    "</button>" +
    "</section>" +
    // Administration — the only block that can grow, so it comes last.
    '<section class="group" data-group="admin" hidden>' +
    '<div class="group-head">' +
    '<h3 class="group-title" data-i18n="set.manageGroup">' +
    staticText("set.manageGroup") +
    "</h3>" +
    '<span class="aside-note" data-i18n="set.adminOnly">' +
    staticText("set.adminOnly") +
    "</span>" +
    "</div>" +
    '<button type="button" class="row is-link" data-role="admin">' +
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
      els.identYou.hidden = true;
      els.identRole.textContent = "";
      els.identSep.hidden = true;
      els.identRemark.textContent = "";
      return;
    }

    var ident = core.identity(me);
    els.identName.textContent = ident.name;
    els.identId.hidden = ident.unnamed;
    els.identId.textContent = ident.unnamed ? "" : "· " + ident.idLabel;
    els.identUnnamed.hidden = !ident.unnamed;
    els.identUnnamed.textContent = ident.unnamed ? t("identity.unnamed") : "";
    els.identYou.hidden = false;
    els.identRole.textContent = t(me.role === "admin" ? "role.admin" : "role.user");

    var remark = String(me.remark || "").trim();
    els.identRemark.textContent = remark;
    els.identRemark.hidden = remark === "";
    els.identSep.hidden = remark === "";
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
    var admin = isAdmin();
    var status = state.status;
    var me = state.me || {};

    // Build facts are public to every role (About lives on the root page); the
    // deployment facts behind webhook/users stay administrator-only.
    var version = me.version || (admin && status && status.version ? status.version : "");
    els.version.hidden = !version;
    els.version.textContent = version;

    els.rows.build.hidden = false;
    els.rows.webhook.hidden = !admin;
    els.rows.users.hidden = !admin;

    var buildAt = me.buildTime || (admin && status && status.buildTime ? status.buildTime : "");
    var build = buildAt ? dateTime(buildAt) : "";
    if (!build && buildAt) {
      // The server sends "unknown time" for an unstamped build; the formatter
      // cannot parse it, so show the raw value instead of an empty row.
      build = String(buildAt);
    }
    els.aboutBuild.textContent = build || "—";
    els.aboutMode.textContent = accessMode()
      ? restricted()
        ? t("set.accessStateRestricted")
        : t("set.accessStateOpen")
      : "—";

    if (admin) {
      var webhook = status && status.webhook;
      els.aboutWebhook.textContent = webhook ? String(webhook.url || "") : t("about.webhookOff");
      els.aboutUsers.textContent = admin && status && status.counts ? num(status.counts.users) : "—";
    } else {
      els.aboutWebhook.textContent = "";
      els.aboutUsers.textContent = "";
    }

    var username = state.stats && state.stats.botUsername ? String(state.stats.botUsername) : "";
    els.help.hidden = username === "";

    els.aboutError.innerHTML = "";
    if (admin && state.statusState === "error") {
      inlineError(els.aboutError, "about.loadFailed", function () {
        return loadStatus().then(renderAll);
      });
    }
  }

  function renderError() {
    els.error.innerHTML = "";
    if (state.meState === "error") {
      inlineError(els.error, "set.profileFailed", function () {
        return load(true);
      });
    }
  }

  function renderAdminGroup() {
    els.adminGroup.hidden = !isAdmin();
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
    renderAdminGroup();
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

  function loadStatus() {
    if (!isAdmin()) {
      state.status = null;
      state.statusState = "idle";
      return Promise.resolve();
    }

    state.statusState = "loading";
    return api()
      .adminStatus()
      .then(function (payload) {
        state.status = payload && typeof payload === "object" ? payload : null;
        state.statusState = "ready";
        state.statusError = null;
      })
      .catch(function (err) {
        // Keep the last snapshot; a failed refresh must not blank the page.
        state.statusState = "error";
        state.statusError = err;
      });
  }

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
        return loadStatus();
      })
      .then(renderAll);
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
      identYou: section.querySelector('[data-slot="ident-you"]'),
      identRole: section.querySelector('[data-slot="ident-role"]'),
      identSep: section.querySelector('[data-slot="ident-sep"]'),
      identRemark: section.querySelector('[data-slot="ident-remark"]'),
      accessState: section.querySelector('[data-slot="access-state"]'),
      languageValue: section.querySelector('[data-slot="language-value"]'),
      appearanceValue: section.querySelector('[data-slot="appearance-value"]'),
      reduceMotion: section.querySelector('[data-role="reduce-motion"]'),
      version: section.querySelector('[data-slot="version"]'),
      aboutError: section.querySelector('[data-slot="about-error"]'),
      aboutBuild: section.querySelector('[data-slot="about-build"]'),
      aboutMode: section.querySelector('[data-slot="about-mode"]'),
      aboutWebhook: section.querySelector('[data-slot="about-webhook"]'),
      aboutUsers: section.querySelector('[data-slot="about-users"]'),
      help: section.querySelector('[data-role="help"]'),
      adminGroup: section.querySelector('[data-group="admin"]'),
      rows: {
        build: section.querySelector('[data-row="build"]'),
        webhook: section.querySelector('[data-row="webhook"]'),
        users: section.querySelector('[data-row="users"]'),
      },
    };
  }

  function bind() {
    core.bindRefresh(els.screen, reload);

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

  core.registerView(VIEW, { mount: mount, onLang: onLang });
})(window);
