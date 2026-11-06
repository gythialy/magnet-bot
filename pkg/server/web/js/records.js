/*
 * records.js — the Records view (read only).
 *
 * Alarm hits used to live under Alarms, stacked below the keyword editor, and a
 * row opened the bot chat through a t.me link: the client jumped to the system
 * browser and then back into Telegram just to see one record. They are a
 * browsing surface, not a setting, so they get their own tab and the detail
 * opens as a sheet built from data the page already has.
 *
 * The page is on the shared v4 shell vocabulary: topbar -> view-head ->
 * controls band -> row list -> sheet. Records pages with the API's nextOffset
 * cursor; "Load more" disappears once the cursor is null. A monotonic request
 * token keeps a slow response from a superseded search from overwriting the
 * newer result, and onLang only rewrites text nodes so the draft, focus, open
 * sheet and scroll survive a language switch.
 */
(function (global) {
  "use strict";

  var core = global.MagnetApp;
  if (!core) {
    // index.html loads js/core.js first; without it there is no transport and no
    // view registry, so this script must not pretend to render anything.
    return;
  }

  var i18n = global.MagnetI18n;
  var VIEW = "records";
  var SECTION_ID = "view-records";
  var PAGE_SIZE = 20;
  var SEARCH_DEBOUNCE_MS = 300;
  // Only Telegram deep links are handed to openTelegramLink. Anything else (a
  // compromised API, a field whose meaning changes later) is dropped rather than
  // passed to a client method that trusts its argument.
  var TELEGRAM_LINK_PREFIX = "https://t.me/";
  // Source pages are ordinary web targets; only http(s) is ever rendered as a
  // link, so a javascript: value from the API can never be followed.
  var WEB_URL = /^https?:\/\//i;

  var els = null;
  var state = {
    items: [],
    q: "",
    nextOffset: null,
    status: "idle",
    loaded: false,
    seq: 0,
    total: 0,
    updatedAt: "",
    errorStatus: 0,
  };
  var sheet = { el: null, item: null, title: null, body: null, actions: null, lastFocus: null };

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
    return i18n ? i18n.date(value) : "";
  }

  function time(value) {
    return i18n ? i18n.time(value) : "";
  }

  function esc(value) {
    return core.escapeHTML(value);
  }

  var TEMPLATE =
    '<div class="screen" data-role="screen">' +
    core.topbarHTML() +
    '<div class="view-head">' +
    '<div class="view-head-row">' +
    "<div>" +
    '<h2 class="view-title" data-i18n="records.title">' + esc(t("records.title")) + "</h2>" +
    '<p class="view-sub" data-slot="sub"></p>' +
    "</div>" +
    "</div>" +
    "</div>" +
    '<div class="controls-band">' +
    '<label class="search">' +
    core.icon("search", "icon-sm") +
    '<input type="search" data-role="search" data-i18n-ph="records.searchPh"' +
    ' data-i18n-aria="records.searchPh" aria-label="' + esc(t("records.searchPh")) + '"' +
    ' placeholder="' + esc(t("records.searchPh")) + '"' +
    ' autocomplete="off" autocapitalize="off" spellcheck="false" enterkeyhint="search" />' +
    "</label>" +
    "</div>" +
    '<div data-slot="loading">' +
    '<p class="label state-label" role="status" data-i18n="common.loading">' +
    esc(t("common.loading")) +
    "</p>" +
    '<div class="sk-block" aria-hidden="true"><span class="sk w60"></span><span class="sk w40"></span></div>' +
    '<div class="sk-block" aria-hidden="true"><span class="sk w80"></span><span class="sk w40"></span></div>' +
    '<div class="sk-block" aria-hidden="true"><span class="sk w60"></span><span class="sk w40"></span></div>' +
    "</div>" +
    '<div class="inline-error" data-slot="error" role="alert" hidden>' +
    core.icon("alert") +
    '<div class="msg">' +
    '<span data-slot="error-msg"></span>' +
    '<span class="sub" data-slot="error-sub" hidden></span>' +
    "</div>" +
    '<button type="button" class="btn-ghost btn-sm" data-role="retry"' +
    ' data-i18n="common.retry">' + esc(t("common.retry")) + "</button>" +
    "</div>" +
    '<div class="empty-block" data-slot="empty" hidden>' +
    '<p class="empty-title" data-slot="empty-title"></p>' +
    "</div>" +
    '<div class="list" data-slot="list" hidden></div>' +
    '<div class="list-foot" data-slot="more" hidden>' +
    '<button type="button" class="btn-wide" data-role="load-more"' +
    ' data-i18n="records.loadMore">' + esc(t("records.loadMore")) + "</button>" +
    "</div>" +
    '<div class="scrim" data-slot="scrim" aria-hidden="true" hidden></div>' +
    '<div class="sheet" data-slot="sheet" role="dialog" aria-modal="true"' +
    ' aria-labelledby="record-sheet-title" hidden>' +
    '<div class="sheet-grab" aria-hidden="true"></div>' +
    '<div class="sheet-head">' +
    "<div>" +
    '<h2 class="sheet-title" id="record-sheet-title" data-slot="sheet-title"></h2>' +
    '<p class="sheet-kicker" data-i18n="records.detailTitle">' +
    esc(t("records.detailTitle")) +
    "</p>" +
    "</div>" +
    '<button type="button" class="icon-btn" data-role="sheet-close"' +
    ' data-i18n-aria="aria.close" aria-label="' + esc(t("aria.close")) + '">' +
    core.icon("close") +
    "</button>" +
    "</div>" +
    '<div class="sheet-body" data-slot="sheet-body"></div>' +
    '<div class="sheet-actions" data-slot="sheet-actions"></div>' +
    "</div>" +
    "</div>";

  /* Small helpers --------------------------------------------------------- */

  function text(value) {
    return value == null ? "" : String(value).trim();
  }

  function safeTelegramLink(value) {
    var raw = text(value);
    return raw.indexOf(TELEGRAM_LINK_PREFIX) === 0 ? raw : "";
  }

  function safeWebLink(value) {
    var raw = text(value);
    return WEB_URL.test(raw) ? raw : "";
  }

  function nextOffsetOf(payload) {
    var value = payload && payload.nextOffset;
    return typeof value === "number" && isFinite(value) && value >= 0 ? value : null;
  }

  function normalizeRecords(payload) {
    var raw = payload && Array.isArray(payload.items) ? payload.items : [];
    var result = [];

    for (var i = 0; i < raw.length; i++) {
      var item = raw[i];
      if (!item || typeof item !== "object") {
        continue;
      }
      result.push({
        businessId: text(item.businessId),
        creditName: text(item.creditName),
        creditCode: text(item.creditCode),
        startDate: item.startDate,
        endDate: item.endDate,
        title: text(item.title),
        detailReason: text(item.detailReason),
        handleDepartment: text(item.handleDepartment),
        handleUnit: text(item.handleUnit),
        handleResult: text(item.handleResult),
        pageUrl1: safeWebLink(item.pageUrl1),
        pageUrl2: safeWebLink(item.pageUrl2),
        noticeId: text(item.noticeId),
        originNoticeId: text(item.originNoticeId),
        link: safeTelegramLink(item.link),
      });
    }

    return result;
  }

  /* Rendering ------------------------------------------------------------- */

  function rowHTML(item) {
    var name = item.creditName || t("records.unknownCredit");
    var start = date(item.startDate);
    var datePart = start
      ? '<span class="sep" aria-hidden="true">\u00b7</span>' +
        '<span class="num" data-row="date">' + esc(start) + "</span>"
      : "";

    return (
      '<button type="button" class="row is-link" data-id="' + esc(item.businessId) + '">' +
      '<span class="row-main">' +
      '<span class="row-lead" data-row="name">' + esc(name) + "</span>" +
      '<span class="row-meta">' +
      '<span class="num" data-row="id">' + esc(item.businessId) + "</span>" +
      datePart +
      "</span>" +
      "</span>" +
      core.icon("chevron", "icon-sm") +
      "</button>"
    );
  }

  function renderList() {
    if (!els) {
      return;
    }
    var html = "";
    for (var i = 0; i < state.items.length; i++) {
      html += rowHTML(state.items[i]);
    }
    els.list.innerHTML = html;
  }

  /* The sub line carries the count and the clock; the list below changes, this
     strip only fades, so switching a search never reflows the page. */
  function renderSub() {
    if (!els) {
      return;
    }
    var shown = state.items.length;
    var total = state.total > shown ? state.total : shown;
    var placeholder = (state.status === "loading" || state.status === "error") && shown === 0;
    var textValue = placeholder ? "\u2014" : num(total) + " " + plural("unit.records", total);
    if (!placeholder && state.updatedAt) {
      textValue += " \u00b7 " + t("today.updated", { time: time(state.updatedAt) });
    }
    els.sub.textContent = textValue;
  }

  function renderStates() {
    if (!els) {
      return;
    }

    var skeleton = state.status === "loading" && state.items.length === 0;
    var empty = state.status === "ready" && state.items.length === 0;
    var failed = state.status === "error";

    els.loading.hidden = !skeleton;
    els.empty.hidden = !empty;
    els.list.hidden = state.items.length === 0;
    els.error.hidden = !failed;

    if (failed) {
      renderError();
    }
    if (empty) {
      els.emptyTitle.textContent = state.q
        ? t("records.emptyMatch", { q: state.q })
        : t("records.empty");
    }
  }

  function renderError() {
    els.errorMsg.textContent =
      state.errorStatus === 429 ? t("app.rateLimited") : t("records.loadFailed");

    // A failed page keeps the rows already read; the sub line says how old the
    // list is instead of pretending it is empty.
    var sub =
      state.items.length > 0 && state.updatedAt
        ? t("lib.stillShowing", { time: time(state.updatedAt) })
        : "";
    els.errorSub.textContent = sub;
    els.errorSub.hidden = !sub;
  }

  function renderMore() {
    if (!els) {
      return;
    }
    var hasMore = state.nextOffset !== null && state.items.length > 0;
    els.more.hidden = !hasMore;
    els.loadMore.disabled = state.status === "loading" || state.status === "error";
    els.loadMore.setAttribute("aria-busy", state.status === "loading" ? "true" : "false");
    els.loadMore.textContent =
      state.status === "loading" ? t("common.loading") : t("records.loadMore");
  }

  function render() {
    renderSub();
    renderList();
    renderStates();
    renderMore();
  }

  /* Detail sheet ---------------------------------------------------------- */

  function setSheetField(field, value) {
    var node = sheet.body.querySelector('[data-field="' + field + '"]');
    if (node) {
      node.textContent = value;
    }
  }

  function sheetRow(labelKey, value, field) {
    return (
      '<div class="sheet-row">' +
      '<span class="label sheet-key" data-i18n="' + labelKey + '">' + esc(t(labelKey)) + "</span>" +
      '<span class="sheet-val"' + (field ? ' data-field="' + field + '"' : "") + ">" +
      value +
      "</span>" +
      "</div>"
    );
  }

  function sheetLinkRow(labelKey, label, url) {
    return (
      '<div class="sheet-row">' +
      '<span class="label sheet-key" data-i18n="' + labelKey + '">' + esc(t(labelKey)) + "</span>" +
      '<span class="sheet-val">' +
      '<a class="sheet-link" href="' + esc(url) + '" data-url="' + esc(url) + '"' +
      ' target="_blank" rel="noopener noreferrer">' + esc(label) + "</a>" +
      "</span>" +
      "</div>"
    );
  }

  function renderSheet(item) {
    var start = date(item.startDate);
    var end = date(item.endDate);

    sheet.title.textContent = item.creditName || t("records.unknownCredit");

    var rows = "";
    rows += item.title
      ? '<p class="sheet-lede">' + esc(item.title) + "</p>"
      : "";
    if (item.creditCode) {
      rows += sheetRow("records.field.creditCode", esc(item.creditCode));
    }
    if (item.businessId) {
      rows += sheetRow("records.field.businessId", esc(item.businessId));
    }
    if (start) {
      rows += sheetRow("records.field.startDate", esc(start), "startDate");
    }
    if (end) {
      rows += sheetRow("records.field.endDate", esc(end), "endDate");
    }
    if (item.handleDepartment) {
      rows += sheetRow("records.field.handleDepartment", esc(item.handleDepartment));
    }
    if (item.handleUnit) {
      rows += sheetRow("records.field.handleUnit", esc(item.handleUnit));
    }
    if (item.detailReason) {
      rows += sheetRow("records.field.detailReason", esc(item.detailReason));
    }
    if (item.handleResult) {
      rows += sheetRow("records.field.handleResult", esc(item.handleResult));
    }
    if (item.pageUrl1) {
      rows += sheetLinkRow(
        "records.field.notice",
        item.noticeId || t("records.field.notice"),
        item.pageUrl1
      );
    }
    if (item.pageUrl2) {
      rows += sheetLinkRow(
        "records.field.sourcePage",
        item.originNoticeId || t("records.field.sourcePage"),
        item.pageUrl2
      );
    }

    sheet.body.innerHTML = rows;

    var actions = "";
    if (item.link) {
      actions +=
        '<button type="button" class="btn is-grow" data-role="sheet-open"' +
        ' data-i18n="records.openInChat">' + esc(t("records.openInChat")) + "</button>";
    }
    actions +=
      '<button type="button" class="btn-ghost" data-role="sheet-dismiss"' +
      ' data-i18n="common.close">' + esc(t("common.close")) + "</button>";
    sheet.actions.innerHTML = actions;

    var open = sheet.actions.querySelector('[data-role="sheet-open"]');
    if (open) {
      open.addEventListener("click", function () {
        // Deep links stay inside Telegram: openLink would hand them to the
        // system browser and bounce back.
        core.openTelegramLink(item.link);
        closeSheet();
      });
    }
    sheet.actions
      .querySelector('[data-role="sheet-dismiss"]')
      .addEventListener("click", closeSheet);

    // Links are ordinary web targets, so they keep the browser.
    Array.prototype.forEach.call(sheet.body.querySelectorAll("a.sheet-link"), function (anchor) {
      anchor.addEventListener("click", function (event) {
        event.preventDefault();
        core.openLink(anchor.getAttribute("data-url"));
      });
    });
  }

  function openSheet(item) {
    sheet.item = item;
    sheet.lastFocus = document.activeElement;
    renderSheet(item);
    sheet.el.hidden = false;
    els.scrim.hidden = false;
    document.body.classList.add("is-sheet-open");
    if (sheet.body.scrollTop) {
      sheet.body.scrollTop = 0;
    }
    var first = sheet.actions.querySelector("button");
    if (first) {
      first.focus({ preventScroll: true });
    }
  }

  function closeSheet() {
    if (!sheet.el || sheet.el.hidden) {
      return;
    }

    var returnTo = sheet.lastFocus;
    sheet.el.hidden = true;
    els.scrim.hidden = true;
    document.body.classList.remove("is-sheet-open");
    sheet.item = null;
    sheet.lastFocus = null;

    if (returnTo && typeof returnTo.focus === "function") {
      try {
        returnTo.focus({ preventScroll: true });
      } catch (err) {
        returnTo.focus();
      }
    }
  }

  /* Data ------------------------------------------------------------------ */

  function loadRecords(reset) {
    if (!reset && state.nextOffset === null) {
      return Promise.resolve();
    }
    if (!core.state || !core.state.initData || !global.MagnetAPI) {
      return Promise.resolve();
    }

    // Bumping the token before the request means any response still in flight
    // belongs to a superseded query and is dropped when it lands.
    var token = (state.seq += 1);
    state.status = "loading";
    state.errorStatus = 0;
    if (reset) {
      state.items = [];
      state.total = 0;
      state.nextOffset = null;
      renderList();
    }
    renderSub();
    renderStates();
    renderMore();

    var offset = reset ? 0 : state.nextOffset;

    return Promise.resolve()
      .then(function () {
        return global.MagnetAPI.alarmRecords({ q: state.q, offset: offset, limit: PAGE_SIZE });
      })
      .then(function (payload) {
        if (token !== state.seq) {
          return;
        }
        var page = normalizeRecords(payload);
        state.items = reset ? page : state.items.concat(page);
        state.total = payload && typeof payload.total === "number" ? payload.total : state.items.length;
        state.nextOffset = nextOffsetOf(payload);
        state.loaded = true;
        state.status = "ready";
        state.updatedAt = new Date().toISOString();
        render();
      })
      .catch(function (err) {
        if (token !== state.seq) {
          return;
        }
        state.status = "error";
        state.errorStatus = err && err.status ? err.status : 0;
        renderStates();
        renderSub();
        renderMore();
      });
  }

  /* Events ---------------------------------------------------------------- */

  function itemById(id) {
    for (var i = 0; i < state.items.length; i++) {
      if (state.items[i].businessId === id) {
        return state.items[i];
      }
    }
    return null;
  }

  function onListClick(event) {
    var target = event.target;
    var button =
      target && typeof target.closest === "function"
        ? target.closest("button[data-id]")
        : null;
    if (!button) {
      return;
    }

    var item = itemById(button.getAttribute("data-id"));
    if (item) {
      openSheet(item);
    }
  }

  function onKeydown(event) {
    if (event.key === "Escape") {
      closeSheet();
    }
  }

  function build(section) {
    section.innerHTML = TEMPLATE;

    sheet.el = section.querySelector('[data-slot="sheet"]');
    sheet.title = section.querySelector('[data-slot="sheet-title"]');
    sheet.body = section.querySelector('[data-slot="sheet-body"]');
    sheet.actions = section.querySelector('[data-slot="sheet-actions"]');

    return {
      screen: section.querySelector('[data-role="screen"]'),
      search: section.querySelector('[data-role="search"]'),
      sub: section.querySelector('[data-slot="sub"]'),
      loading: section.querySelector('[data-slot="loading"]'),
      error: section.querySelector('[data-slot="error"]'),
      errorMsg: section.querySelector('[data-slot="error-msg"]'),
      errorSub: section.querySelector('[data-slot="error-sub"]'),
      empty: section.querySelector('[data-slot="empty"]'),
      emptyTitle: section.querySelector('[data-slot="empty-title"]'),
      list: section.querySelector('[data-slot="list"]'),
      more: section.querySelector('[data-slot="more"]'),
      loadMore: section.querySelector('[data-role="load-more"]'),
      retry: section.querySelector('[data-role="retry"]'),
      scrim: section.querySelector('[data-slot="scrim"]'),
      sheetClose: section.querySelector('[data-role="sheet-close"]'),
    };
  }

  function bind() {
    // Debounced so typing does not issue one request per keystroke; the trailing
    // call reads the final value, so intermediate keystrokes are skipped.
    var runSearch = core.debounce(function () {
      var value = els.search.value.trim();
      if (value === state.q) {
        return;
      }
      state.q = value;
      loadRecords(true);
    }, SEARCH_DEBOUNCE_MS);

    core.bindRefresh(els.screen, function () {
      loadRecords(true);
    });

    els.search.addEventListener("input", runSearch);
    els.list.addEventListener("click", onListClick);
    els.loadMore.addEventListener("click", function () {
      if (state.status !== "loading") {
        loadRecords(false);
      }
    });
    els.retry.addEventListener("click", function () {
      loadRecords(state.items.length === 0);
    });

    els.sheetClose.addEventListener("click", closeSheet);
    els.scrim.addEventListener("click", closeSheet);
    document.addEventListener("keydown", onKeydown);
  }

  /* Language ------------------------------------------------------------- */

  function syncRowText() {
    var rows = els.list.querySelectorAll("button.row[data-id]");
    for (var i = 0; i < rows.length; i++) {
      var row = rows[i];
      var item = itemById(row.getAttribute("data-id"));
      if (!item) {
        continue;
      }
      var name = row.querySelector('[data-row="name"]');
      if (name) {
        name.textContent = item.creditName || t("records.unknownCredit");
      }
      var start = row.querySelector('[data-row="date"]');
      if (start) {
        start.textContent = date(item.startDate);
      }
    }
  }

  function syncSheetText() {
    if (!sheet.item || sheet.el.hidden) {
      return;
    }
    sheet.title.textContent = sheet.item.creditName || t("records.unknownCredit");
    setSheetField("startDate", date(sheet.item.startDate));
    setSheetField("endDate", date(sheet.item.endDate));
  }

  // Rewrites only the text this view owns; the shell has already applied the
  // static data-i18n nodes, and nothing here rebuilds the list.
  function onLang() {
    if (!els) {
      return;
    }
    renderSub();
    renderStates();
    renderMore();
    syncRowText();
    syncSheetText();
  }

  /* View ------------------------------------------------------------------ */

  function mount() {
    var section = document.getElementById(SECTION_ID);
    if (!section) {
      return;
    }

    if (!els) {
      els = build(section);
      bind();
    }

    // Returning to the tab with a sheet open should show the list, not a stale
    // dialog over the current data.
    closeSheet();

    if (!core.state || !core.state.initData) {
      els.loading.hidden = false;
      els.list.hidden = true;
      els.empty.hidden = true;
      els.error.hidden = true;
      els.more.hidden = true;
      return;
    }

    if (!state.loaded || core.state.isStale(VIEW)) {
      loadRecords(true);
      return;
    }

    render();
  }

  core.registerView(VIEW, { mount: mount, onLang: onLang });
})(window);
