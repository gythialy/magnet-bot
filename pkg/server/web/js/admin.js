/*
 * admin.js — the administrator console (#view-admin).
 *
 * A sub-page of Settings, so the root screen stays short: access control, the
 * member list with its paging and per-row actions, and the recently denied
 * requests. Nothing here is reachable for a non-administrator; the server also
 * enforces that, and this view renders "admins only" instead of pretending.
 *
 * Writes refresh the section they touch. Guard refusals (409) are translated by
 * their machine-readable reason, never by the fixed English message.
 */
(function (global) {
  "use strict";

  var core = global.MagnetApp;
  if (!core) {
    return;
  }

  var i18n = global.MagnetI18n;
  var VIEW = "admin";
  var SECTION_ID = "view-admin";
  var PAGE_SIZE = 20;
  var SEARCH_DEBOUNCE_MS = 300;
  var MODE_RESTRICTED = "restricted";
  var MODE_OFF = "off";

  var state = {
    me: null,
    meState: "idle",
    access: { mode: "", state: "idle", saving: false, envManaged: false },
    people: {
      q: "",
      items: [],
      total: 0,
      nextOffset: null,
      status: "idle",
      loaded: false,
      error: false,
      seq: 0,
    },
    status: { data: null, state: "idle", error: null },
    denied: { expanded: false, allowed: Object.create(null), pending: Object.create(null) },
    loaded: false,
  };

  var els = null;
  var sheet = null;

  /* Helpers -------------------------------------------------------------- */

  function t(key, vars) {
    return i18n ? i18n.t(key, vars) : key;
  }

  function plural(key, count) {
    return i18n ? i18n.plural(key, count) : key;
  }

  function num(value) {
    return i18n ? i18n.num(value) : String(value);
  }

  function date(value) {
    return i18n ? i18n.date(value) : String(value || "");
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

  function numberOrZero(value) {
    var parsed = Number(value);
    return isFinite(parsed) ? parsed : 0;
  }

  function isAdmin() {
    return Boolean(state.me && state.me.role === "admin");
  }

  function isSelf(item) {
    return Boolean(state.me && String(state.me.userId) === String(item.userId));
  }

  function restricted() {
    return state.access.mode === MODE_RESTRICTED;
  }

  /** The number of accounts on the list, once either request has told us. */
  function listedTotal() {
    if (state.people.loaded) {
      return state.people.total;
    }
    if (state.status.data && state.status.data.counts) {
      return numberOrZero(state.status.data.counts.users);
    }
    return null;
  }

  function guardKey(err) {
    if (!err || err.status !== 409) {
      return "";
    }
    if (err.code === "guard_self") {
      return "set.guardSelf";
    }
    if (err.code === "guard_last_admin") {
      return "set.guardLastAdmin";
    }
    if (err.code === "guard_env_managed") {
      return "set.guardEnvManaged";
    }
    return "set.updateFailed";
  }

  function toastError(err, fallbackKey) {
    if (err && err.status === 429) {
      core.toast(t("app.rateLimited"), "error");
      return;
    }
    core.toast(t(guardKey(err) || fallbackKey || "app.error"), "error");
  }

  /* Template ------------------------------------------------------------- */

  function staticText(key) {
    return esc(t(key));
  }

  var TEMPLATE =
    '<div class="screen" data-role="screen">' +
    core.subTopbarHTML("set.manage") +
    '<div data-slot="me-error"></div>' +
    '<div data-slot="loading" class="loading-block">' +
    '<div class="sk-block" aria-hidden="true"><span class="sk w60"></span><span class="sk w40"></span></div>' +
    '<div class="sk-block" aria-hidden="true"><span class="sk w40"></span><span class="sk w80"></span></div>' +
    "</div>" +
    '<div class="admin-gate" data-slot="gate" hidden>' +
    '<div class="empty-block">' +
    '<p class="empty-title" data-i18n="app.adminOnly">' +
    staticText("app.adminOnly") +
    "</p>" +
    '<p class="empty-note" data-i18n="app.adminOnlyHint">' +
    staticText("app.adminOnlyHint") +
    "</p>" +
    "</div>" +
    "</div>" +
    '<div data-slot="content" hidden>' +
    '<section class="group" data-group="access">' +
    '<div class="group-head">' +
    '<h3 class="group-title" data-i18n="set.access">' +
    staticText("set.access") +
    "</h3>" +
    '<span class="aside-note" data-i18n="set.adminOnly">' +
    staticText("set.adminOnly") +
    "</span>" +
    "</div>" +
    '<div class="access-card">' +
    '<div class="access-state">' +
    core.icon("shield") +
    '<span data-slot="access-state"></span>' +
    "</div>" +
    '<p class="access-hint" data-slot="access-hint"></p>' +
    '<div class="access-switch-row">' +
    '<span class="field-label" data-i18n="set.restrictLabel">' +
    staticText("set.restrictLabel") +
    "</span>" +
    '<label class="switch"><input type="checkbox" data-role="access-toggle" data-i18n-aria="aria.restrict" aria-label="' +
    esc(t("aria.restrict")) +
    '" /><span class="switch-track"></span></label>' +
    "</div>" +
    '<p class="env-note" data-slot="access-env" data-i18n="set.envManaged" hidden>' +
    staticText("set.envManaged") +
    "</p>" +
    "</div>" +
    "</section>" +
    '<div data-slot="status-error"></div>' +
    '<section class="group" data-group="people">' +
    '<div class="group-head">' +
    '<h3 class="group-title"><span data-i18n="set.people">' +
    staticText("set.people") +
    '</span> <span class="count-chip num" data-slot="people-count" hidden></span></h3>' +
    '<button type="button" class="btn-ghost btn-sm admin-add" data-role="add">' +
    core.icon("plus", "icon-sm") +
    "<span data-i18n=\"set.add\">" +
    staticText("set.add") +
    "</span></button>" +
    "</div>" +
    '<div class="sheet-search is-inline">' +
    '<label class="search">' +
    core.icon("search", "icon-sm") +
    '<input type="search" data-role="search" autocomplete="off" data-i18n-ph="set.searchPeople" data-i18n-aria="set.searchPeople" aria-label="' +
    esc(t("set.searchPeople")) +
    '" />' +
    "</label>" +
    "</div>" +
    '<div data-slot="people-error"></div>' +
    '<div class="list" data-slot="people-list" hidden></div>' +
    '<p class="label state-label" data-slot="people-state" role="status" hidden></p>' +
    '<div class="empty-block" data-slot="people-empty" hidden>' +
    '<p class="empty-title" data-slot="people-empty-title"></p>' +
    '<p class="empty-note" data-slot="people-empty-note" hidden></p>' +
    "</div>" +
    '<div class="list-foot is-tight" data-slot="people-foot" hidden>' +
    '<button type="button" class="btn-wide" data-role="more"></button>' +
    "</div>" +
    '<button type="button" class="row is-link" data-role="denied" aria-expanded="false">' +
    '<span class="row-lead is-caption"><span data-i18n="set.recentDenied">' +
    staticText("set.recentDenied") +
    '</span> <span class="count-chip num" data-slot="denied-count" hidden></span></span>' +
    core.icon("chevron") +
    "</button>" +
    '<div class="denied-block" data-slot="denied-block" hidden>' +
    '<div data-slot="denied-list"></div>' +
    "</div>" +
    "</section>" +
    // Service status — the deployment facts the root page used to carry. They
    // belong next to the console that changes them, not under "About".
    '<section class="group" data-group="service" hidden>' +
    '<div class="group-head">' +
    '<h3 class="group-title" data-i18n="set.service">' +
    staticText("set.service") +
    "</h3>" +
    "</div>" +
    '<div class="row">' +
    '<div class="row-main">' +
    '<span class="row-lead" data-i18n="about.version">' +
    staticText("about.version") +
    "</span>" +
    "</div>" +
    '<span class="row-value num" data-slot="service-version"></span>' +
    "</div>" +
    '<div class="row">' +
    '<div class="row-main">' +
    '<span class="row-lead" data-i18n="about.webhook">' +
    staticText("about.webhook") +
    "</span>" +
    '<span class="row-meta" data-slot="service-webhook-error" hidden></span>' +
    "</div>" +
    '<span class="row-value" data-slot="service-webhook"></span>' +
    "</div>" +
    '<p class="group-note num" data-slot="service-counts"></p>' +
    "</section>" +
    "</div>" +
    "</div>";

  /* Inline errors -------------------------------------------------------- */

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

  function pair(el, key, vars) {
    if (!sheet) {
      el.textContent = t(key, vars);
      return el;
    }
    sheet.pairs.push({ el: el, key: key, vars: vars || null, fn: null });
    el.textContent = t(key, vars);
    return el;
  }

  function pairFn(el, fn) {
    if (!sheet) {
      el.textContent = fn();
      return el;
    }
    sheet.pairs.push({ el: el, key: "", vars: null, fn: fn });
    el.textContent = fn();
    return el;
  }

  function sheetLabels() {
    if (!sheet) {
      return;
    }
    sheet.title.textContent = t(sheet.titleKey, sheet.titleVars);
    sheet.kicker.hidden = !sheet.kickerKey;
    sheet.kicker.textContent = sheet.kickerKey ? t(sheet.kickerKey) : "";
    sheet.close.setAttribute("aria-label", t("aria.close"));
    sheet.cancel.textContent = t("common.cancel");
    sheet.confirm.textContent = t(sheet.pending ? sheet.pendingKey : sheet.confirmKey);
    sheet.confirm.disabled = sheet.pending || sheet.disabled;
    sheet.cancel.disabled = sheet.pending;
    sheet.close.disabled = sheet.pending;
    if (sheet.noteKey) {
      sheet.note.textContent = t(sheet.noteKey);
    }
    if (sheet.noteFn) {
      sheet.note.textContent = sheet.noteFn();
    }
    sheet.pairs.forEach(function (entry) {
      entry.el.textContent = entry.fn ? entry.fn() : t(entry.key, entry.vars);
    });
  }

  function showNote(key) {
    if (!sheet) {
      return;
    }
    sheet.noteKey = key;
    sheet.noteFn = null;
    sheet.note.hidden = false;
    sheet.note.textContent = t(key);
  }

  function clearNote() {
    if (!sheet) {
      return;
    }
    sheet.noteKey = "";
    sheet.noteFn = null;
    sheet.note.hidden = true;
    sheet.note.textContent = "";
  }

  function setPending(pending) {
    if (!sheet) {
      return;
    }
    sheet.pending = pending;
    sheetLabels();
  }

  function setDisabled(disabled) {
    if (!sheet) {
      return;
    }
    sheet.disabled = disabled;
    sheetLabels();
  }

  function onSheetConfirm() {
    if (!sheet || sheet.pending || sheet.disabled || typeof sheet.onConfirm !== "function") {
      return;
    }
    clearNote();
    var result;
    try {
      result = sheet.onConfirm();
    } catch (err) {
      toastError(err, "app.error");
      return;
    }
    if (!result || typeof result.then !== "function") {
      return;
    }
    setPending(true);
    result.then(
      function () {
        setPending(false);
      },
      function (err) {
        setPending(false);
        toastError(err, "app.error");
      }
    );
  }

  function openSheet(config) {
    closeSheet();

    var scrim = document.createElement("div");
    scrim.className = "scrim";

    var el = document.createElement("section");
    el.className = "sheet";
    el.setAttribute("role", "dialog");
    el.setAttribute("aria-modal", "true");
    el.setAttribute("aria-labelledby", "admin-sheet-title");
    el.innerHTML =
      '<div class="sheet-grab" aria-hidden="true"></div>' +
      '<div class="sheet-head"><div>' +
      '<h3 class="sheet-title" id="admin-sheet-title" data-role="title"></h3>' +
      '<p class="sheet-kicker" data-role="kicker"></p>' +
      "</div>" +
      '<button type="button" class="icon-btn" data-role="close">' +
      core.icon("close") +
      "</button></div>" +
      '<div class="sheet-body" data-role="body"></div>' +
      '<p class="sheet-error" data-role="note" role="alert" hidden></p>' +
      '<div class="sheet-actions">' +
      '<button type="button" class="btn-ghost is-grow" data-role="cancel"></button>' +
      '<button type="button" class="btn is-grow" data-role="confirm"></button>' +
      "</div>";

    sheet = {
      el: el,
      scrim: scrim,
      title: el.querySelector('[data-role="title"]'),
      kicker: el.querySelector('[data-role="kicker"]'),
      body: el.querySelector('[data-role="body"]'),
      note: el.querySelector('[data-role="note"]'),
      close: el.querySelector('[data-role="close"]'),
      cancel: el.querySelector('[data-role="cancel"]'),
      confirm: el.querySelector('[data-role="confirm"]'),
      pairs: [],
      titleKey: config.titleKey,
      titleVars: config.titleVars || null,
      kickerKey: config.kickerKey || "",
      confirmKey: config.confirmKey || "common.save",
      pendingKey: config.pendingKey || "common.saving",
      pending: false,
      disabled: false,
      noteKey: "",
      noteFn: null,
      field: config.field || null,
      lastFocus: document.activeElement,
      onConfirm: config.onConfirm,
    };

    if (config.danger) {
      sheet.confirm.classList.add("btn-danger");
    }

    document.body.appendChild(scrim);
    document.body.appendChild(el);

    if (typeof config.build === "function") {
      config.build(sheet.body);
    }

    sheet.close.addEventListener("click", closeSheet);
    sheet.cancel.addEventListener("click", closeSheet);
    sheet.confirm.addEventListener("click", onSheetConfirm);
    scrim.addEventListener("click", closeSheet);

    if (config.requireValue && sheet.field) {
      setDisabled(sheet.field.value.trim() === "");
      sheet.field.addEventListener("input", function () {
        setDisabled(sheet.field.value.trim() === "");
      });
    }

    sheetLabels();

    var focusTarget =
      typeof config.focus === "function"
        ? config.focus()
        : config.focus || (config.danger ? sheet.cancel : sheet.confirm);
    if (focusTarget && typeof focusTarget.focus === "function") {
      focusTarget.focus();
    }
  }

  function textInput(value, placeholderKey) {
    var input = document.createElement("input");
    input.type = "text";
    input.className = "dialog-input";
    input.value = value || "";
    if (placeholderKey) {
      input.setAttribute("data-i18n-ph", placeholderKey);
      input.setAttribute("placeholder", t(placeholderKey));
    }
    return input;
  }

  function textArea(placeholderKey) {
    var input = document.createElement("textarea");
    input.className = "dialog-textarea";
    input.rows = 3;
    if (placeholderKey) {
      input.setAttribute("data-i18n-ph", placeholderKey);
      input.setAttribute("placeholder", t(placeholderKey));
    }
    return input;
  }

  function roleSelect(value) {
    var select = document.createElement("select");
    select.className = "dialog-select";
    [
      { value: "user", key: "role.user" },
      { value: "admin", key: "role.admin" },
    ].forEach(function (spec) {
      var option = document.createElement("option");
      option.value = spec.value;
      pair(option, spec.key);
      select.appendChild(option);
    });
    select.value = value === "admin" ? "admin" : "user";
    return select;
  }

  function field(labelKey, control) {
    var wrap = document.createElement("label");
    wrap.className = "form-field";
    var caption = document.createElement("span");
    caption.className = "label";
    pair(caption, labelKey);
    wrap.appendChild(caption);
    wrap.appendChild(control);
    return wrap;
  }

  function switchField(labelKey, checked) {
    var wrap = document.createElement("div");
    wrap.className = "form-field form-switch";

    var caption = document.createElement("span");
    caption.className = "field-label";
    pair(caption, labelKey);

    var label = document.createElement("label");
    label.className = "switch";
    var input = document.createElement("input");
    input.type = "checkbox";
    input.checked = checked === true;
    input.setAttribute("data-i18n-aria", "set.enabledField");
    input.setAttribute("aria-label", t("set.enabledField"));
    var track = document.createElement("span");
    track.className = "switch-track";
    label.appendChild(input);
    label.appendChild(track);

    wrap.appendChild(caption);
    wrap.appendChild(label);
    return { el: wrap, input: input };
  }

  function sheetRow(keyLabel, valueKey, vars) {
    var row = document.createElement("div");
    row.className = "sheet-row";
    var key = document.createElement("span");
    key.className = "label sheet-key";
    pair(key, keyLabel);
    var value = document.createElement("span");
    value.className = "sheet-val";
    pair(value, valueKey, vars);
    row.appendChild(key);
    row.appendChild(value);
    return row;
  }

  function identityLine(user) {
    var line = document.createElement("p");
    line.className = "sheet-lede";
    pairFn(line, function () {
      return core.identity(user).full;
    });
    return line;
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

  /* Access --------------------------------------------------------------- */

  function applyMode(mode) {
    state.access.mode = mode === MODE_RESTRICTED ? MODE_RESTRICTED : MODE_OFF;
  }

  function renderAccess() {
    var restrictedMode = restricted();
    var total = listedTotal();

    if (!state.access.mode) {
      els.accessState.textContent = "—";
    } else {
      els.accessState.textContent = restrictedMode
        ? t("set.accessRestricted")
        : t("set.accessStateOpen");
    }

    if (restrictedMode && total === null) {
      els.accessHint.hidden = true;
      els.accessHint.textContent = "";
    } else {
      els.accessHint.hidden = false;
      els.accessHint.textContent = restrictedMode
        ? t("set.accessHintRestricted", { count: num(total || 0) })
        : t("set.accessHintOpen");
    }

    els.accessToggle.checked = restrictedMode;
    // Turning the guard off names how many people the list holds, so the switch
    // waits for that count instead of opening a confirm that says "0".
    var countKnown = total !== null;
    els.accessToggle.disabled =
      state.access.saving ||
      state.access.envManaged ||
      state.access.mode === "" ||
      (restrictedMode && !countKnown);
    els.accessEnv.hidden = !state.access.envManaged;
  }

  function patchMode(mode) {
    state.access.saving = true;
    renderAccess();

    return api()
      .adminSetAccessMode(mode)
      .then(function (payload) {
        state.access.saving = false;
        applyMode(payload && payload.accessMode ? payload.accessMode : mode);
        renderAll();
        core.state.invalidate("settings");
        core.state.invalidate("members");
        core.me(true).catch(function () {
          // A stale cached identity only affects the next screen's label.
        });
        loadStatus();
        core.toast(t(mode === MODE_RESTRICTED ? "set.modeRestricted" : "set.modeOpen"), "success");
        return true;
      })
      .catch(function (err) {
        state.access.saving = false;
        renderAll();
        toastError(err, "set.modeFailed");
        return false;
      });
  }

  function onAccessToggle() {
    if (!els.accessToggle || state.access.saving) {
      return;
    }
    if (els.accessToggle.checked) {
      patchMode(MODE_RESTRICTED);
      return;
    }

    // Turning the guard off asks once and names the blast radius; until the
    // sheet is confirmed the switch keeps showing the mode in effect.
    els.accessToggle.checked = true;
    openAccessConfirm();
  }

  function openAccessConfirm() {
    var total = listedTotal() || 0;

    openSheet({
      titleKey: "accessConfirm.title",
      kickerKey: "accessConfirm.kicker",
      confirmKey: "accessConfirm.confirm",
      pendingKey: "common.saving",
      danger: true,
      build: function (body) {
        var lede = document.createElement("p");
        lede.className = "sheet-lede";
        pair(lede, "accessConfirm.body");

        body.appendChild(lede);
        body.appendChild(sheetRow("accessConfirm.who", "accessConfirm.whoVal", { count: num(total) }));
        body.appendChild(sheetRow("accessConfirm.undo", "accessConfirm.undoVal"));
      },
      onConfirm: function () {
        state.access.saving = true;
        renderAccess();
        return api()
          .adminSetAccessMode(MODE_OFF)
          .then(function (payload) {
            state.access.saving = false;
            applyMode(payload && payload.accessMode ? payload.accessMode : MODE_OFF);
            renderAll();
            closeSheet();
            core.state.invalidate("settings");
            core.state.invalidate("members");
            core.me(true).catch(function () {
              // Best effort; the settings root also reads the mode from status.
            });
            loadStatus();
            core.toast(t("set.modeOpen"), "success");
          })
          .catch(function (err) {
            state.access.saving = false;
            renderAccess();
            var key = guardKey(err);
            if (key) {
              showNote(key);
              return;
            }
            toastError(err, "set.modeFailed");
          });
      },
    });
  }

  /* Members -------------------------------------------------------------- */

  function normalizeItems(payload) {
    var raw = payload && Array.isArray(payload.items) ? payload.items : [];
    return raw
      .filter(function (item) {
        return item && typeof item === "object";
      })
      .map(function (item) {
        var counts = item.keywordCounts || {};
        return {
          userId: item.userId,
          remark: typeof item.remark === "string" ? item.remark : "",
          name: typeof item.name === "string" ? item.name : "",
          username: typeof item.username === "string" ? item.username : "",
          role: item.role === "admin" ? "admin" : "user",
          enabled: item.enabled === true,
          fromEnv: item.fromEnv === true,
          createdAt: item.createdAt || "",
          lastSeen: item.lastSeen || null,
          keywordCounts: {
            project: numberOrZero(counts.project),
            alarm: numberOrZero(counts.alarm),
          },
        };
      });
  }

  function findMember(userId) {
    var wanted = String(userId);
    for (var i = 0; i < state.people.items.length; i++) {
      if (String(state.people.items[i].userId) === wanted) {
        return state.people.items[i];
      }
    }
    return null;
  }

  function memberKeywords(item) {
    return item.keywordCounts.project + item.keywordCounts.alarm;
  }

  function memberRow(item) {
    var ident = core.identity(item);
    var self = isSelf(item);
    var keywords = memberKeywords(item);
    var sep = '<span class="sep">·</span>';

    var lead =
      '<span class="row-lead"><span class="row-name">' +
      esc(ident.name) +
      "</span>" +
      (ident.unnamed
        ? '<span class="name-note">' + esc(t("identity.unnamed")) + "</span>"
        : '<span class="row-id num">' + esc(ident.idLabel) + "</span>") +
      (self ? '<span class="name-note">' + esc(t("set.you")) + "</span>" : "") +
      "</span>";

    var meta =
      esc(t(item.role === "admin" ? "role.admin" : "role.user")) +
      sep +
      esc(t(item.enabled ? "set.active" : "set.disabled"));
    if (item.fromEnv) {
      meta += sep + esc(t("set.envManaged"));
    }
    meta +=
      sep +
      '<span class="num">' +
      esc(num(keywords)) +
      "</span> " +
      esc(plural("unit.keywords", keywords));

    var actions = '<div class="row-actions">';
    actions +=
      '<button type="button" class="icon-btn" data-action="view" data-user-id="' +
      esc(String(item.userId)) +
      '" data-i18n-aria="aria.viewList" aria-label="' +
      esc(t("aria.viewList")) +
      '">' +
      core.icon("eye", "icon-sm") +
      "</button>";

    if (!self && !item.fromEnv) {
      actions +=
        '<button type="button" class="icon-btn" data-action="edit" data-user-id="' +
        esc(String(item.userId)) +
        '" data-i18n-aria="aria.editAccess" aria-label="' +
        esc(t("aria.editAccess")) +
        '">' +
        core.icon("edit", "icon-sm") +
        "</button>";
    }
    if (!self && !item.fromEnv && item.enabled) {
      actions +=
        '<button type="button" class="icon-btn is-danger" data-action="revoke" data-user-id="' +
        esc(String(item.userId)) +
        '" data-i18n-aria="aria.revoke" aria-label="' +
        esc(t("aria.revoke")) +
        '">' +
        core.icon("trash", "icon-sm") +
        "</button>";
    }
    actions += "</div>";

    return (
      '<div class="row" data-user-id="' +
      esc(String(item.userId)) +
      '"><div class="row-main">' +
      lead +
      '<span class="row-meta">' +
      meta +
      "</span></div>" +
      actions +
      "</div>"
    );
  }

  function renderPeople() {
    var total = listedTotal();
    els.peopleCount.hidden = total === null;
    els.peopleCount.textContent = total === null ? "" : num(total);

    var firstLoad = state.people.status === "loading" && state.people.items.length === 0;
    els.peopleState.hidden = !firstLoad;
    if (firstLoad) {
      els.peopleState.textContent = t("common.loading");
    }

    els.peopleError.innerHTML = "";
    if (state.people.status === "error") {
      inlineError(els.peopleError, "set.loadPeopleFailed", function () {
        return loadPeople(true);
      });
    }

    if (firstLoad) {
      var skeleton = "";
      for (var i = 0; i < 3; i++) {
        skeleton +=
          '<div class="sk-block" aria-hidden="true"><span class="sk w60"></span><span class="sk w40"></span></div>';
      }
      els.peopleList.innerHTML = skeleton;
      els.peopleList.hidden = false;
      els.peopleEmpty.hidden = true;
      els.peopleFoot.hidden = true;
      return;
    }

    var empty = state.people.status === "ready" && state.people.items.length === 0;
    els.peopleList.hidden = state.people.items.length === 0;
    els.peopleList.innerHTML = state.people.items.map(memberRow).join("");
    els.peopleEmpty.hidden = !empty;

    if (empty) {
      var searching = state.people.q !== "";
      els.peopleEmptyTitle.textContent = searching ? t("pick.noResults") : t("set.noPeople");
      els.peopleEmptyNote.hidden = searching;
      els.peopleEmptyNote.textContent = searching ? "" : t("set.addHint");
    }

    var remaining = total === null ? 0 : Math.max(total - state.people.items.length, 0);
    var more = state.people.items.length > 0 && state.people.nextOffset !== null;
    els.peopleFoot.hidden = !more;
    if (more) {
      els.more.textContent = remaining > 0 ? t("set.loadMore", { count: num(remaining) }) : t("common.loadMore");
      els.more.disabled = state.people.status === "loading";
    }
  }

  function loadPeople(reset) {
    if (!isAdmin()) {
      return Promise.resolve();
    }
    if (!reset && state.people.nextOffset === null) {
      return Promise.resolve();
    }

    var offset = reset ? 0 : state.people.nextOffset;
    var seq = ++state.people.seq;

    if (reset) {
      state.people.items = [];
      state.people.nextOffset = null;
    }
    state.people.status = "loading";
    state.people.error = false;
    renderAll();

    return api()
      .adminUsers({ q: state.people.q, offset: offset, limit: PAGE_SIZE })
      .then(function (payload) {
        if (seq !== state.people.seq) {
          return;
        }
        var items = normalizeItems(payload);
        state.people.items = reset ? items : state.people.items.concat(items);
        state.people.total = numberOrZero(payload && payload.total);
        state.people.nextOffset =
          payload && typeof payload.nextOffset === "number" && isFinite(payload.nextOffset)
            ? payload.nextOffset
            : null;
        if (items.length === 0) {
          state.people.nextOffset = null;
        }
        state.people.status = "ready";
        state.people.loaded = true;
        renderAll();
      })
      .catch(function () {
        if (seq !== state.people.seq) {
          return;
        }
        state.people.status = "error";
        state.people.error = true;
        renderAll();
      });
  }

  /* Edit, add, revoke ---------------------------------------------------- */

  function openAddSheet() {
    var textarea;
    var role;
    var remark;

    openSheet({
      titleKey: "set.addTitle",
      confirmKey: "common.add",
      pendingKey: "common.adding",
      requireValue: true,
      build: function (body) {
        textarea = textArea("set.addHint");
        textarea.setAttribute("data-i18n-aria", "set.userIds");
        textarea.setAttribute("aria-label", t("set.userIds"));
        role = roleSelect("user");
        remark = textInput("", "set.remarkField");

        body.appendChild(field("set.userIds", textarea));
        body.appendChild(field("set.roleField", role));
        body.appendChild(field("set.remarkField", remark));

        sheet.field = textarea;
      },
      focus: function () {
        return textarea;
      },
      onConfirm: function () {
        var ids = normalizeIds(textarea.value);
        if (!ids) {
          setDisabled(true);
          return;
        }
        return api()
          .adminAddUsers({ userIds: ids, role: role.value, remark: remark.value.trim() })
          .then(function () {
            closeSheet();
            core.toast(t("set.granted"), "success");
            core.state.invalidate("settings");
            core.state.invalidate("members");
            refreshAfterWrite();
          })
          .catch(function (err) {
            var key = guardKey(err);
            if (key) {
              showNote(key);
              return;
            }
            toastError(err, "set.addFailed");
          });
      },
    });
  }

  // The server splits a batch on commas; pasted spaces and newlines are folded
  // here so a multi-line list does not arrive as one unparsable id.
  function normalizeIds(raw) {
    return String(raw || "")
      .split(/[\s,]+/)
      .map(function (token) {
        return token.trim();
      })
      .filter(Boolean)
      .join(",");
  }

  function openEditSheet(item) {
    var remark;
    var role;
    var enabled;

    openSheet({
      titleKey: "set.editTitle",
      confirmKey: "common.save",
      pendingKey: "common.saving",
      build: function (body) {
        remark = textInput(item.remark, "set.remarkField");
        role = roleSelect(item.role);
        var toggle = switchField("set.enabledField", item.enabled);

        body.appendChild(identityLine(item));
        body.appendChild(field("set.remarkField", remark));
        body.appendChild(field("set.roleField", role));
        body.appendChild(toggle.el);
        enabled = toggle.input;
      },
      focus: function () {
        return remark;
      },
      onConfirm: function () {
        return api()
          .adminUpdateUser(item.userId, {
            remark: remark.value.trim(),
            role: role.value,
            enabled: enabled.checked,
          })
          .then(function () {
            closeSheet();
            core.toast(t("set.accessUpdated"), "success");
            core.state.invalidate("settings");
            core.state.invalidate("members");
            refreshAfterWrite();
          })
          .catch(function (err) {
            var key = guardKey(err);
            if (key) {
              showNote(key);
              return;
            }
            toastError(err, "set.updateFailed");
          });
      },
    });
  }

  function openRevokeSheet(item) {
    var ident = core.identity(item);
    var name = ident.name;

    openSheet({
      titleKey: "set.revokeTitle",
      titleVars: { name: name },
      confirmKey: "set.revoke",
      pendingKey: "common.saving",
      danger: true,
      build: function (body) {
        body.appendChild(identityLine(item));
        var bodyText = document.createElement("p");
        bodyText.className = "sheet-lede";
        pair(bodyText, "set.revokeBody");
        body.appendChild(bodyText);
      },
      onConfirm: function () {
        return api()
          .adminDeleteUser(item.userId)
          .then(function () {
            closeSheet();
            core.toast(t("set.accessRevoked"), "success");
            core.state.invalidate("settings");
            core.state.invalidate("members");
            removeMember(item.userId);
            refreshAfterWrite();
          })
          .catch(function (err) {
            var key = guardKey(err);
            if (key) {
              showNote(key);
              return;
            }
            toastError(err, "set.revokeFailed");
          });
      },
    });
  }

  function removeMember(userId) {
    var wanted = String(userId);
    state.people.items = state.people.items.filter(function (item) {
      return String(item.userId) !== wanted;
    });
    if (state.people.total > 0) {
      state.people.total -= 1;
    }
    renderAll();
  }

  function refreshAfterWrite() {
    return Promise.all([loadPeople(true), loadStatus()]).then(renderAll);
  }

  /* Recently denied ------------------------------------------------------ */

  function deniedItems() {
    var data = state.status.data;
    var raw = data && Array.isArray(data.recentDenied) ? data.recentDenied : [];
    return raw.filter(function (item) {
      return item && item.userId != null && !state.denied.allowed[String(item.userId)];
    });
  }

  function renderDenied() {
    var items = deniedItems();

    els.denied.setAttribute("aria-expanded", state.denied.expanded ? "true" : "false");
    els.deniedBlock.hidden = !state.denied.expanded;
    els.deniedCount.hidden = items.length === 0;
    els.deniedCount.textContent = num(items.length);

    if (!state.denied.expanded) {
      els.deniedList.innerHTML = "";
      return;
    }

    if (items.length === 0) {
      els.deniedList.innerHTML = "";
      var empty = document.createElement("p");
      empty.className = "denied-empty";
      empty.textContent = state.status.state === "loading" ? t("common.loading") : t("set.noDenied");
      els.deniedList.appendChild(empty);
      return;
    }

    els.deniedList.innerHTML = items
      .map(function (item) {
        var ident = core.identity(item);
        var pending = state.denied.pending[String(item.userId)] === true;
        return (
          '<div class="denied-row" data-user-id="' +
          esc(String(item.userId)) +
          '"><div class="denied-main">' +
          '<span class="denied-name">' +
          esc(ident.name) +
          "</span>" +
          '<span class="denied-meta"><span class="num">' +
          esc(ident.idLabel) +
          '</span><span class="sep">·</span><span>' +
          esc(date(item.at)) +
          "</span></span></div>" +
          '<button type="button" class="btn-ghost btn-sm" data-action="allow" data-user-id="' +
          esc(String(item.userId)) +
          '"' +
          (pending ? " disabled" : "") +
          ">" +
          esc(t("set.allow")) +
          "</button></div>"
        );
      })
      .join("");
  }

  function allowDenied(userId) {
    var key = String(userId);
    if (state.denied.pending[key]) {
      return;
    }
    state.denied.pending[key] = true;
    renderDenied();

    api()
      .adminAddUsers({ userIds: key, role: "user" })
      .then(function () {
        delete state.denied.pending[key];
        state.denied.allowed[key] = true;
        core.toast(t("set.granted"), "success");
        core.state.invalidate("settings");
        core.state.invalidate("members");
        renderAll();
        refreshAfterWrite();
      })
      .catch(function (err) {
        delete state.denied.pending[key];
        renderDenied();
        toastError(err, "set.grantFailed");
      });
  }

  /* Status --------------------------------------------------------------- */

  function loadStatus() {
    if (!isAdmin()) {
      return Promise.resolve();
    }

    state.status.state = "loading";
    return api()
      .adminStatus()
      .then(function (payload) {
        state.status.data = payload && typeof payload === "object" ? payload : null;
        state.status.state = "ready";
        state.status.error = null;
        if (!state.access.mode && state.status.data && state.status.data.accessMode) {
          applyMode(state.status.data.accessMode);
        }
        renderAll();
      })
      .catch(function (err) {
        // Keep the last snapshot; a failed refresh must not blank the console.
        state.status.state = "error";
        state.status.error = err;
        renderAll();
      });
  }

  // Deployment facts: which build is running, how Telegram reaches the bot, and
  // how much the store holds. Read-only; the console is where it is acted on.
  function renderService() {
    var status = state.status.data;
    var ready = Boolean(status) && state.status.state !== "idle";

    els.serviceGroup.hidden = !ready;
    if (!ready) {
      return;
    }

    var version = typeof status.version === "string" ? status.version.trim() : "";
    var built = typeof status.buildTime === "string" ? status.buildTime.trim() : "";
    var builtLabel = built ? date(built) : "";
    els.serviceVersion.textContent = [version, builtLabel].filter(Boolean).join(" \u00b7 ") || "\u2014";

    var webhook = status.webhook;
    var url = webhook && typeof webhook.url === "string" ? webhook.url.trim() : "";
    els.serviceWebhook.textContent = url || t("about.webhookOff");
    var lastError = webhook && typeof webhook.lastError === "string" ? webhook.lastError.trim() : "";
    els.serviceWebhookError.textContent = lastError
      ? t("about.webhookLastError") + " " + lastError
      : "";
    els.serviceWebhookError.hidden = lastError === "";

    var counts = status.counts || {};
    var parts = [];
    if (typeof counts.keywords === "number") {
      parts.push(t("about.keywords") + " " + num(counts.keywords));
    }
    if (typeof counts.histories === "number") {
      parts.push(t("about.history") + " " + num(counts.histories));
    }
    if (typeof counts.alarms === "number") {
      parts.push(t("about.alarmRecords") + " " + num(counts.alarms));
    }
    els.serviceCounts.textContent = parts.join(" \u00b7 ");
    els.serviceCounts.hidden = parts.length === 0;
  }

  function renderStatusError() {
    els.statusError.innerHTML = "";
    if (isAdmin() && state.status.state === "error") {
      inlineError(els.statusError, "about.loadFailed", function () {
        return loadStatus().then(renderAll);
      });
    }
  }

  function loadAccess() {
    state.access.state = "loading";
    return api()
      .adminSettings()
      .then(function (payload) {
        state.access.state = "ready";
        if (payload && payload.accessMode) {
          applyMode(payload.accessMode);
        }
        if (payload && (payload.fromEnv === true || payload.envManaged === true)) {
          state.access.envManaged = true;
        }
        renderAll();
      })
      .catch(function () {
        state.access.state = "error";
        renderAll();
      });
  }

  /* Me ------------------------------------------------------------------- */

  function ensureMe(force) {
    if (!force && state.meState === "ready" && state.me) {
      return Promise.resolve(state.me);
    }
    return core
      .me(Boolean(force))
      .then(function (me) {
        state.me = me;
        state.meState = "ready";
        if (me && !state.access.mode && me.accessMode) {
          applyMode(me.accessMode);
        }
        return me;
      })
      .catch(function () {
        state.meState = "error";
        return null;
      });
  }

  /* Rendering ------------------------------------------------------------ */

  function renderMeError() {
    els.meError.innerHTML = "";
    if (state.meState === "error") {
      inlineError(els.meError, "app.error", function () {
        return ensureMe(true).then(function () {
          if (isAdmin()) {
            return loadAll();
          }
          renderAll();
          return null;
        });
      });
    }
  }

  function renderAll() {
    if (!els) {
      return;
    }

    var known = state.meState === "ready";
    var admin = isAdmin();

    els.loading.hidden = known || state.meState === "error";
    els.gate.hidden = !(known && !admin);
    els.content.hidden = !(known && admin);

    if (known && admin) {
      renderAccess();
      renderPeople();
      renderStatusError();
      renderService();
      renderDenied();
    }

    renderMeError();
    sheetLabels();
  }

  function loadAll() {
    if (!isAdmin()) {
      return Promise.resolve();
    }
    return Promise.all([loadAccess(), loadPeople(true), loadStatus()]).then(function () {
      state.loaded = true;
      renderAll();
    });
  }

  /* Wiring --------------------------------------------------------------- */

  function build(section) {
    section.innerHTML = TEMPLATE;
    els = {
      screen: section.querySelector('[data-role="screen"]'),
      meError: section.querySelector('[data-slot="me-error"]'),
      loading: section.querySelector('[data-slot="loading"]'),
      gate: section.querySelector('[data-slot="gate"]'),
      content: section.querySelector('[data-slot="content"]'),
      accessState: section.querySelector('[data-slot="access-state"]'),
      accessHint: section.querySelector('[data-slot="access-hint"]'),
      accessToggle: section.querySelector('[data-role="access-toggle"]'),
      accessEnv: section.querySelector('[data-slot="access-env"]'),
      statusError: section.querySelector('[data-slot="status-error"]'),
      peopleCount: section.querySelector('[data-slot="people-count"]'),
      search: section.querySelector('[data-role="search"]'),
      peopleError: section.querySelector('[data-slot="people-error"]'),
      peopleList: section.querySelector('[data-slot="people-list"]'),
      peopleState: section.querySelector('[data-slot="people-state"]'),
      peopleEmpty: section.querySelector('[data-slot="people-empty"]'),
      peopleEmptyTitle: section.querySelector('[data-slot="people-empty-title"]'),
      peopleEmptyNote: section.querySelector('[data-slot="people-empty-note"]'),
      peopleFoot: section.querySelector('[data-slot="people-foot"]'),
      more: section.querySelector('[data-role="more"]'),
      denied: section.querySelector('[data-role="denied"]'),
      deniedCount: section.querySelector('[data-slot="denied-count"]'),
      deniedBlock: section.querySelector('[data-slot="denied-block"]'),
      deniedList: section.querySelector('[data-slot="denied-list"]'),
      serviceGroup: section.querySelector('[data-group="service"]'),
      serviceVersion: section.querySelector('[data-slot="service-version"]'),
      serviceWebhook: section.querySelector('[data-slot="service-webhook"]'),
      serviceWebhookError: section.querySelector('[data-slot="service-webhook-error"]'),
      serviceCounts: section.querySelector('[data-slot="service-counts"]'),
      add: section.querySelector('[data-role="add"]'),
    };
  }

  function bind() {
    core.bindBack(els.screen, function () {
      core.go("settings");
    });

    els.accessToggle.addEventListener("change", onAccessToggle);
    els.add.addEventListener("click", openAddSheet);

    var runSearch = core.debounce(function () {
      state.people.q = els.search.value.trim();
      loadPeople(true);
    }, SEARCH_DEBOUNCE_MS);
    els.search.addEventListener("input", runSearch);

    els.more.addEventListener("click", function () {
      if (state.people.status === "ready") {
        loadPeople(false);
      }
    });

    els.peopleList.addEventListener("click", function (event) {
      var button = event.target.closest("button[data-action]");
      if (!button) {
        return;
      }
      var item = findMember(button.getAttribute("data-user-id"));
      if (!item) {
        return;
      }
      var action = button.getAttribute("data-action");
      if (action === "view") {
        core.state.viewingUser = item;
        core.go("library");
      } else if (action === "edit") {
        openEditSheet(item);
      } else if (action === "revoke") {
        openRevokeSheet(item);
      }
    });

    els.denied.addEventListener("click", function () {
      state.denied.expanded = !state.denied.expanded;
      renderDenied();
    });

    els.deniedList.addEventListener("click", function (event) {
      var button = event.target.closest("button[data-action]");
      if (!button || button.getAttribute("data-action") !== "allow") {
        return;
      }
      allowDenied(button.getAttribute("data-user-id"));
    });

    document.addEventListener("keydown", onKeydown);
  }

  function onLang() {
    if (!els) {
      return;
    }
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

    return ensureMe(false).then(function () {
      if (!isAdmin()) {
        renderAll();
        return null;
      }
      if (!state.loaded || core.state.isStale(VIEW)) {
        return loadAll();
      }
      renderAll();
      return null;
    });
  }

  core.registerView(VIEW, { mount: mount, onLang: onLang });
})(window);
