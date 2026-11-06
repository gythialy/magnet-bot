/*
 * today.js — the Today view.
 *
 * Owns #view-today: the running head, the statistics strip, the range/search
 * band, the notice list, the "Load more" pager and the notice detail sheet.
 *
 * The structure is the settled v4 vocabulary from css/base.css: topbar ->
 * runhead -> stats -> controls-band -> entry list. This file builds every node
 * on the first mount and reuses them afterwards; index.html only carries the
 * skeleton it replaces.
 *
 * Behaviour notes that are easy to get wrong later:
 *   - core.js calls mount() once when this script registers (before init() has
 *     run, so before initData exists) and again from route(). The first call
 *     only draws the skeleton; the second one fetches.
 *   - core.js deletes the "stale" flag before calling mount(), so mount() has
 *     to decide on its own whether it owes a request. It fetches when nothing
 *     has loaded yet, when the previous attempt failed, or when someone
 *     invalidated the view between renders.
 *   - A refresh keeps the current rows: if it fails the list stays readable and
 *     an inline error names how old the list is. Range and search changes do
 *     clear first, because the old rows would belong to a different query.
 *   - onLang only rewrites text nodes. The search draft, focus, the open sheet,
 *     the scroll position and the rendered rows all survive a language switch.
 *   - Titles, project codes, the search text and any server date string are
 *     user data: they are escaped before being interpolated into innerHTML.
 *     URLs are never interpolated at all — an entry carries an index and the
 *     link is read back from state, and only https:// links are kept.
 *
 * No build step, no third-party code, no external requests.
 */
(function (global) {
  "use strict";

  var app = global.MagnetApp;
  if (!app || typeof app.registerView !== "function") {
    // Without core.js there is no shell to register against; refusing here is
    // better than half-rendering a view the router will never mount.
    return;
  }

  var i18n = global.MagnetI18n;

  var VIEW = "today";
  var RANGES = ["today", "7d", "all"];
  var PAGE_LIMIT = 20;
  var SEARCH_DEBOUNCE_MS = 300;
  var STORAGE_SEEN = "magnet.today.seenAt";

  // Project codes. The first pattern is the bot's own codeRegx from
  // pkg/handler/commands_handler.go, so the app and /today agree on what counts
  // as a code: four digits, an upper-case word, then an upper-case/digit tail,
  // wrapped in ASCII or full-width parentheses.
  var CODE_IN_BRACKETS = /[（(]([0-9]{4}-[A-Z]+-[A-Z0-9]+)[）)]/;
  // Some rows carry the code without parentheses. The second segment must
  // contain at least one letter, which is what keeps a date such as 2025-10-31
  // from being mistaken for a code.
  var CODE_BARE = /(?:^|[^0-9A-Z-])([0-9]{4}-[A-Z]+-[A-Z0-9]+)(?![0-9A-Z-])/;

  var HTTPS_ONLY = /^https:\/\//i;

  // Empty copy per range, keyed by the same words the segment uses.
  var RANGE_EMPTY_KEYS = {
    today: "today.empty",
    "7d": "today.empty.7d",
    all: "today.empty.all",
  };

  // Conversion formats the bot understands. The label is the file format's own
  // name (never translated); the aria key is the accessible verb phrase.
  var CONVERT_FORMATS = {
    pdf: { label: "PDF", icon: "pdf", aria: "aria.sendPdf" },
    png: { label: "PNG", icon: "png", aria: "aria.sendImage" },
  };

  // core.toast removes itself after 3s. The conversion flow needs a 2s
  // "in progress" prompt and a 3.5s acknowledgement, so those two toasts are
  // built locally with the same markup instead of fighting the shell timer.
  var CONVERT_PENDING_TOAST_MS = 2000;
  var CONVERT_DONE_TOAST_MS = 3500;

  // Nodes, queried or created once. null until build() succeeds.
  var refs = null;
  // The document-level Esc listener is installed once, not once per mount.
  var escapeWired = false;

  var state = {
    range: "today",
    q: "",
    items: [],
    // Dedupe keys of everything already in items; offset paging over a live
    // table can repeat a row when new notices land between two pages.
    seen: Object.create(null),
    nextOffset: null,
    total: 0,
    updatedAt: "",
    lastSeenAt: readSeenAt(),
    loading: false,
    loaded: false,
    failed: false,
    errorStatus: 0,
    detailIndex: -1,
    lastFocus: null,
  };

  // Bumped per request so a slow response for an old query can never overwrite
  // the list for the query the user is actually looking at.
  var requestSeq = 0;

  // Conversions currently submitted, keyed by "format\0url". A notice can have
  // one PDF and one PNG in flight at the same time; a second tap on the same
  // pair is ignored until the first answers.
  var converting = Object.create(null);

  var stats = {
    history: null,
    keywords: null,
    related: null,
    alarms: null,
    loaded: false,
    loading: false,
  };

  /* Small helpers --------------------------------------------------------- */

  function t(key, vars) {
    return i18n ? i18n.t(key, vars) : key;
  }

  function plural(key, count) {
    return i18n ? i18n.plural(key, count) : key;
  }

  function num(value) {
    return i18n ? i18n.num(value) : String(value);
  }

  function dateTime(value) {
    return i18n ? i18n.dateTime(value) : "";
  }

  function time(value) {
    return i18n ? i18n.time(value) : "";
  }

  function escape(value) {
    return app.escapeHTML(value);
  }

  function element(tag, className) {
    var node = document.createElement(tag);
    if (className) {
      node.className = className;
    }
    return node;
  }

  function toCount(value) {
    if (typeof value === "number" && isFinite(value)) {
      return Math.round(value);
    }
    if (typeof value === "string" && value !== "" && isFinite(Number(value))) {
      return Math.round(Number(value));
    }
    return null;
  }

  function extractProjectCode(title) {
    var text = String(title == null ? "" : title);
    var match = CODE_IN_BRACKETS.exec(text);
    if (!match) {
      match = CODE_BARE.exec(text);
    }
    return match ? match[1] : "";
  }

  // Anything that is not https:// is dropped rather than handed to
  // Telegram's openLink, so a javascript: or http: row can never be launched.
  function safeURL(value) {
    var raw = String(value == null ? "" : value).trim();
    return HTTPS_ONLY.test(raw) ? raw : "";
  }

  // The newest visit baseline, in local storage. Marks are "new since the last
  // time this device opened Today"; a first visit stores a baseline and marks
  // nothing, which keeps the very first screen calm.
  function readSeenAt() {
    try {
      var raw = global.localStorage.getItem(STORAGE_SEEN);
      var parsed = Number(raw);
      return isFinite(parsed) && parsed > 0 ? parsed : 0;
    } catch (err) {
      return 0;
    }
  }

  function storeSeenAt(value) {
    try {
      global.localStorage.setItem(STORAGE_SEEN, String(value));
    } catch (err) {
      // Storage failure only costs the unread marks, not the list.
    }
  }

  /* Model ---------------------------------------------------------------- */

  function normalizeItem(raw) {
    if (!raw || typeof raw !== "object") {
      return null;
    }

    var title = typeof raw.title === "string" ? raw.title : "";
    var updatedAt = typeof raw.updatedAt === "string" ? raw.updatedAt : "";
    var parsed = Date.parse(updatedAt);

    return {
      url: safeURL(raw.url),
      title: title,
      code: extractProjectCode(title),
      updatedAt: updatedAt,
      at: isFinite(parsed) ? parsed : 0,
      // The contract says boolean; tolerate the numeric form a stale page of
      // JSON might still carry.
      hasTenderCode: raw.hasTenderCode === true || raw.hasTenderCode === 1,
      unseen: false,
    };
  }

  function itemKey(item) {
    return item.url || item.title + "\u0000" + item.updatedAt;
  }

  function mergeItems(incoming) {
    for (var i = 0; i < incoming.length; i++) {
      var key = itemKey(incoming[i]);
      if (state.seen[key]) {
        continue;
      }
      state.seen[key] = true;
      state.items.push(incoming[i]);
    }
  }

  function markUnseen(incoming) {
    if (!state.lastSeenAt) {
      return;
    }
    for (var i = 0; i < incoming.length; i++) {
      incoming[i].unseen = incoming[i].at > state.lastSeenAt;
    }
  }

  function rememberSeen() {
    var newest = Date.now();
    for (var i = 0; i < state.items.length; i++) {
      if (state.items[i].at > newest) {
        newest = state.items[i].at;
      }
    }
    state.lastSeenAt = newest;
    storeSeenAt(newest);
  }

  /* Template ------------------------------------------------------------- */

  function statHTML(key, labelKey) {
    return (
      '<div class="stat">' +
      '<span class="stat-v num" data-stat="' + key + '">\u2014</span>' +
      '<span class="label stat-k" data-i18n="' + labelKey + '">' + escape(t(labelKey)) + "</span>" +
      "</div>"
    );
  }

  function metaHTML(item) {
    var parts = [];
    if (item.unseen) {
      parts.push('<span class="mark" aria-hidden="true"></span>');
    }
    parts.push(
      '<span class="num" data-meta="time">' + escape(dateTime(item.updatedAt)) + "</span>"
    );
    if (item.code) {
      parts.push('<span class="sep" aria-hidden="true">\u00b7</span>');
      parts.push('<span class="num" data-meta="code">' + escape(item.code) + "</span>");
    }
    if (item.hasTenderCode) {
      parts.push(
        '<span class="tag">' +
          app.icon("tender") +
          '<span data-meta="tag-label">' + escape(t("today.tagTender")) + "</span>" +
          "</span>"
      );
    }
    return parts.join("");
  }

  function convertButton(index, format, scope) {
    var meta = CONVERT_FORMATS[format];
    return (
      '<button type="button" class="' + (scope === "detail" ? "btn-ghost" : "icon-btn") + '"' +
      ' data-action="convert" data-format="' + format + '"' +
      (scope === "detail" ? ' data-scope="detail"' : ' data-index="' + index + '"') +
      ' data-i18n-aria="' + meta.aria + '" aria-label="' + escape(t(meta.aria)) + '">' +
      app.icon(meta.icon, "icon-sm") +
      (scope === "detail" ? "<span>" + meta.label + "</span>" : "") +
      "</button>"
    );
  }

  function openButton(index) {
    return (
      '<button type="button" class="icon-btn" data-action="open-link"' +
      ' data-index="' + index + '" data-i18n-aria="aria.openNotice"' +
      ' aria-label="' + escape(t("aria.openNotice")) + '">' +
      app.icon("external", "icon-sm") +
      "</button>"
    );
  }

  function entryHTML(item, index) {
    var title = item.title || t("today.noTitle");
    // Conversion and opening need a notice URL; the server re-checks ownership
    // and the host allow-list, so a notice without an https URL gets disabled
    // actions instead of buttons that can only fail.
    return (
      '<article class="entry is-link" data-action="detail" data-index="' + index + '">' +
      '<button type="button" class="entry-body" data-action="detail" data-index="' + index + '"' +
      ' aria-haspopup="dialog">' +
      '<span class="entry-lead">' + escape(title) + "</span>" +
      "</button>" +
      '<div class="entry-foot">' +
      '<div class="entry-meta">' + metaHTML(item) + "</div>" +
      '<div class="entry-actions">' +
      convertButton(index, "pdf", "list") +
      convertButton(index, "png", "list") +
      openButton(index) +
      "</div>" +
      "</div>" +
      "</article>"
    );
  }

  var TEMPLATE =
    '<div class="screen" data-role="screen">' +
    app.topbarHTML() +
    '<div class="runhead">' +
    '<div class="runhead-main">' +
    '<h2 class="runhead-title" data-i18n="today.title">' + escape(t("today.title")) + "</h2>" +
    '<span class="runhead-count num" data-slot="count">\u2014</span>' +
    '<span class="runhead-unit" data-slot="unit"></span>' +
    "</div>" +
    '<span class="runhead-time num" data-slot="updated"></span>' +
    "</div>" +
    '<div class="stats" data-slot="stats">' +
    statHTML("history", "stat.history") +
    statHTML("keywords", "stat.keywords") +
    statHTML("related", "stat.related") +
    statHTML("alarms", "stat.alarms") +
    "</div>" +
    '<div class="controls-band">' +
    '<div class="segmented" role="group" data-i18n-aria="aria.dateRange"' +
    ' aria-label="' + escape(t("aria.dateRange")) + '">' +
    rangeButton("today", true) +
    rangeButton("7d", false) +
    rangeButton("all", false) +
    "</div>" +
    '<label class="search">' +
    app.icon("search", "icon-sm") +
    '<input type="search" data-role="search" data-i18n-ph="today.searchPh"' +
    ' data-i18n-aria="today.searchPh" aria-label="' + escape(t("today.searchPh")) + '"' +
    ' placeholder="' + escape(t("today.searchPh")) + '"' +
    ' autocomplete="off" autocapitalize="off" spellcheck="false" enterkeyhint="search" />' +
    "</label>" +
    "</div>" +
    '<div data-slot="loading">' +
    '<p class="label state-label" role="status" data-i18n="common.loading">' +
    escape(t("common.loading")) +
    "</p>" +
    '<div class="sk-block" aria-hidden="true"><span class="sk w60"></span><span class="sk w40"></span></div>' +
    '<div class="sk-block" aria-hidden="true"><span class="sk w80"></span><span class="sk w40"></span></div>' +
    '<div class="sk-block" aria-hidden="true"><span class="sk w60"></span><span class="sk w40"></span></div>' +
    "</div>" +
    '<div class="inline-error" data-slot="error" role="alert" hidden>' +
    app.icon("alert") +
    '<div class="msg">' +
    '<span data-slot="error-msg"></span>' +
    '<span class="sub" data-slot="error-sub" hidden></span>' +
    "</div>" +
    '<button type="button" class="btn-ghost btn-sm" data-action="retry"' +
    ' data-i18n="common.retry">' + escape(t("common.retry")) + "</button>" +
    "</div>" +
    '<div class="empty-block" data-slot="empty" hidden>' +
    '<p class="empty-title" data-slot="empty-title"></p>' +
    "</div>" +
    '<div class="list" data-slot="list" hidden></div>' +
    '<div class="list-foot" data-slot="more" hidden>' +
    '<button type="button" class="btn-wide" data-action="load-more"' +
    ' data-i18n="today.loadMore">' + escape(t("today.loadMore")) + "</button>" +
    "</div>" +
    '<div class="scrim" data-slot="scrim" aria-hidden="true" hidden></div>' +
    '<div class="sheet" data-slot="detail" role="dialog" aria-modal="true"' +
    ' aria-labelledby="today-detail-title" hidden>' +
    '<div class="sheet-grab" aria-hidden="true"></div>' +
    '<div class="sheet-head">' +
    '<div>' +
    '<h2 class="sheet-title" id="today-detail-title" data-slot="detail-title"></h2>' +
    '<p class="sheet-kicker" data-slot="detail-kicker"></p>' +
    "</div>" +
    '<button type="button" class="icon-btn" data-action="close-detail"' +
    ' data-i18n-aria="aria.close" aria-label="' + escape(t("aria.close")) + '">' +
    app.icon("close") +
    "</button>" +
    "</div>" +
    '<div class="sheet-actions">' +
    '<button type="button" class="btn is-grow" data-action="open-notice"' +
    ' data-i18n="today.open">' + escape(t("today.open")) + "</button>" +
    convertButton(-1, "pdf", "detail") +
    convertButton(-1, "png", "detail") +
    "</div>" +
    "</div>" +
    "</div>";

  function rangeButton(range, active) {
    var key = "today.range." + range;
    return (
      '<button type="button" data-action="range" data-range="' + range + '"' +
      (active ? ' class="is-active" aria-pressed="true"' : ' aria-pressed="false"') +
      ' data-i18n="' + key + '">' + escape(t(key)) + "</button>"
    );
  }

  function build(section) {
    section.innerHTML = TEMPLATE;

    var statNodes = {};
    Array.prototype.forEach.call(section.querySelectorAll("[data-stat]"), function (node) {
      statNodes[node.getAttribute("data-stat")] = node;
    });

    return {
      view: section,
      screen: section.querySelector('[data-role="screen"]'),
      count: section.querySelector('[data-slot="count"]'),
      unit: section.querySelector('[data-slot="unit"]'),
      updated: section.querySelector('[data-slot="updated"]'),
      stats: statNodes,
      segmented: section.querySelector(".segmented"),
      rangeButtons: section.querySelectorAll("[data-action='range']"),
      search: section.querySelector('[data-role="search"]'),
      loading: section.querySelector('[data-slot="loading"]'),
      error: section.querySelector('[data-slot="error"]'),
      errorMsg: section.querySelector('[data-slot="error-msg"]'),
      errorSub: section.querySelector('[data-slot="error-sub"]'),
      empty: section.querySelector('[data-slot="empty"]'),
      emptyTitle: section.querySelector('[data-slot="empty-title"]'),
      list: section.querySelector('[data-slot="list"]'),
      more: section.querySelector('[data-slot="more"]'),
      moreButton: section.querySelector("[data-action='load-more']"),
      scrim: section.querySelector('[data-slot="scrim"]'),
      detail: section.querySelector('[data-slot="detail"]'),
      detailTitle: section.querySelector('[data-slot="detail-title"]'),
      detailKicker: section.querySelector('[data-slot="detail-kicker"]'),
      detailClose: section.querySelector("[data-action='close-detail']"),
    };
  }

  function ensureDOM() {
    var section = document.getElementById("view-today");
    if (!section) {
      return null;
    }
    if (refs && refs.view === section) {
      return refs;
    }

    refs = build(section);
    refs.search.addEventListener("input", app.debounce(applySearch, SEARCH_DEBOUNCE_MS));
    refs.screen.addEventListener("click", onClick);
    refs.scrim.addEventListener("click", closeDetail);
    app.bindRefresh(refs.screen, refresh);

    if (!escapeWired) {
      document.addEventListener("keydown", onKeydown);
      escapeWired = true;
    }

    syncSearchValue();
    syncRangeButtons();
    renderRunhead();
    renderStats();
    return refs;
  }

  /* Rendering ------------------------------------------------------------ */

  function renderList() {
    if (!refs) {
      return;
    }
    var html = "";
    for (var i = 0; i < state.items.length; i++) {
      html += entryHTML(state.items[i], i);
    }
    refs.list.innerHTML = html;
    syncActionButtons();
  }

  function renderRunhead() {
    if (!refs) {
      return;
    }
    var shown = state.items.length;
    var count = state.total > shown ? state.total : shown;
    // Before the first page lands there is no count to show; an em dash keeps
    // the head from claiming "0 notices" while the request is still running.
    var placeholder = (state.loading || state.failed) && shown === 0;
    refs.count.textContent = placeholder ? "\u2014" : num(count);
    refs.unit.textContent = placeholder ? "" : plural("unit.notices", count);
    refs.updated.textContent = state.updatedAt
      ? t("today.updated", { time: time(state.updatedAt) })
      : "";
  }

  function renderStats() {
    if (!refs) {
      return;
    }
    // Counts are server integers, not user text, so textContent is used here:
    // it cannot be parsed as markup no matter what the API returns.
    refs.stats.history.textContent = stats.history === null ? "\u2014" : num(stats.history);
    refs.stats.keywords.textContent = stats.keywords === null ? "\u2014" : num(stats.keywords);
    refs.stats.related.textContent = stats.related === null ? "\u2014" : num(stats.related);
    refs.stats.alarms.textContent = stats.alarms === null ? "\u2014" : num(stats.alarms);
  }

  function renderStates() {
    if (!refs) {
      return;
    }

    var firstLoad = state.loading && state.items.length === 0;
    var empty = !state.loading && !state.failed && state.loaded && state.items.length === 0;

    refs.loading.hidden = !firstLoad;
    refs.empty.hidden = !empty;
    refs.list.hidden = state.items.length === 0;
    refs.error.hidden = !state.failed;

    if (state.failed) {
      renderError();
    }
    if (empty) {
      renderEmpty();
    }
  }

  function renderError() {
    var rateLimited = state.errorStatus === 429;
    refs.errorMsg.textContent = rateLimited ? t("app.rateLimited") : t("today.loadFailed");

    // A failed refresh keeps the readable rows and says how old they are; a
    // first-load failure has nothing to name.
    var sub =
      state.items.length > 0 && state.updatedAt
        ? t("lib.stillShowing", { time: time(state.updatedAt) })
        : "";
    refs.errorSub.textContent = sub;
    refs.errorSub.hidden = !sub;
  }

  function renderEmpty() {
    var key = state.q
      ? "today.emptyMatch"
      : RANGE_EMPTY_KEYS[state.range] || "today.empty";
    refs.emptyTitle.textContent = state.q ? t(key, { q: state.q }) : t(key);
  }

  function renderMore() {
    if (!refs) {
      return;
    }

    var hasMore = state.nextOffset !== null && state.items.length > 0;
    refs.more.hidden = !hasMore;
    refs.moreButton.disabled = state.loading;
    refs.moreButton.setAttribute("aria-busy", state.loading ? "true" : "false");
    refs.moreButton.textContent = state.loading ? t("common.loading") : t("today.loadMore");
  }

  function renderAll() {
    renderRunhead();
    renderStats();
    renderList();
    renderStates();
    renderMore();
  }

  function showSkeleton() {
    if (!refs) {
      return;
    }
    state.loading = true;
    state.failed = false;
    state.loaded = false;
    renderRunhead();
    renderStats();
    renderStates();
    renderMore();
  }

  function syncSearchValue() {
    if (refs && refs.search.value !== state.q) {
      refs.search.value = state.q;
    }
  }

  function syncRangeButtons() {
    if (!refs) {
      return;
    }
    Array.prototype.forEach.call(refs.rangeButtons, function (button) {
      var active = button.getAttribute("data-range") === state.range;
      button.classList.toggle("is-active", active);
      button.setAttribute("aria-pressed", active ? "true" : "false");
    });
  }

  /* Statistics ----------------------------------------------------------- */

  function loadStatistics() {
    if (!refs || stats.loaded || stats.loading || !global.MagnetAPI) {
      return;
    }

    stats.loading = true;

    global.MagnetAPI.statistics()
      .then(function (payload) {
        stats.loading = false;
        stats.loaded = true;
        stats.history = toCount(payload && payload.history);
        stats.keywords = toCount(payload && payload.keywords);
        stats.related = toCount(payload && payload.relatedNotices);
        stats.alarms = toCount(payload && payload.alarmKeywords);
        renderStats();
      })
      .catch(function () {
        // A failed counter must never take the list down with it: the stats
        // stay at "—" and the next mount tries again.
        stats.loading = false;
        renderStats();
      });
  }

  /* Data loading --------------------------------------------------------- */

  function applyPage(payload, append, offset) {
    var rawItems = payload && payload.items;
    var incoming = [];

    if (rawItems && typeof rawItems.length === "number") {
      for (var i = 0; i < rawItems.length; i++) {
        var item = normalizeItem(rawItems[i]);
        if (item) {
          incoming.push(item);
        }
      }
    }

    if (!append) {
      state.items = [];
      state.seen = Object.create(null);
      markUnseen(incoming);
    }
    mergeItems(incoming);

    state.loaded = true;
    state.failed = false;
    state.errorStatus = 0;

    // The head reports the server's total so a partial page still tells the
    // truth about how much is there; the clock tells the reader how stale the
    // column is.
    state.total = payload && typeof payload.total === "number" ? payload.total : state.items.length;
    state.updatedAt = new Date().toISOString();

    var next = payload ? payload.nextOffset : null;
    // Only accept a cursor that actually moves forward, and never offer a
    // "Load more" that would fetch the page we just rendered again.
    if (typeof next === "number" && isFinite(next) && next > offset && incoming.length > 0) {
      state.nextOffset = next;
    } else {
      state.nextOffset = null;
    }

    if (!append) {
      rememberSeen();
    }
  }

  function fetchPage(append) {
    if (!refs) {
      return Promise.resolve();
    }
    if (!app.state || !app.state.initData || !global.MagnetAPI) {
      // init() has not run yet; the router mounts this view again once it has,
      // and that mount is the one allowed to hit the network.
      return Promise.resolve();
    }

    var sequence = ++requestSeq;
    var offset = append && typeof state.nextOffset === "number" ? state.nextOffset : 0;

    state.loading = true;
    state.failed = false;
    state.errorStatus = 0;
    renderStates();
    renderMore();

    return global.MagnetAPI.notices({
      range: state.range,
      q: state.q,
      offset: offset,
      limit: PAGE_LIMIT,
    })
      .then(function (payload) {
        if (sequence !== requestSeq) {
          // A newer query owns the view now; this answer is stale noise.
          return;
        }
        state.loading = false;
        applyPage(payload, append, offset);
        renderAll();
      })
      .catch(function (err) {
        if (sequence !== requestSeq) {
          return;
        }
        state.loading = false;

        if (append) {
          // Keep the rows already on screen and let the user retry the page
          // that failed; a toast is enough for an incremental load.
          renderMore();
          app.toast(t("today.loadMoreFailed"), "error");
          return;
        }

        state.failed = true;
        state.errorStatus = err && err.status ? err.status : 0;
        renderRunhead();
        renderStates();
        renderMore();
      });
  }

  function reload() {
    if (!refs) {
      return Promise.resolve();
    }

    // Clear first: rows from the previous filter would claim to belong to the
    // range or query that is loading now.
    state.items = [];
    state.seen = Object.create(null);
    state.nextOffset = null;
    state.total = 0;
    state.loaded = false;
    state.failed = false;
    state.errorStatus = 0;

    closeDetail();
    renderList();
    renderStates();
    renderMore();
    renderRunhead();

    return fetchPage(false);
  }

  // Refresh keeps the rows: on failure the list stays readable behind the
  // inline error, which is why it does not call reload().
  function refresh() {
    if (!refs || state.loading) {
      return Promise.resolve();
    }
    closeDetail();
    return fetchPage(false);
  }

  function loadMore() {
    if (!refs || state.loading || state.nextOffset === null) {
      return Promise.resolve();
    }
    return fetchPage(true);
  }

  /* Search and range ----------------------------------------------------- */

  function applySearch() {
    if (!refs) {
      return;
    }
    var value = String(refs.search.value || "").trim();
    if (value === state.q) {
      return;
    }
    state.q = value;
    reload();
  }

  function setRange(range) {
    if (!refs || RANGES.indexOf(range) === -1 || range === state.range) {
      return;
    }
    state.range = range;
    syncRangeButtons();
    reload();
  }

  /* Detail sheet --------------------------------------------------------- */

  function detailMeta(item) {
    var parts = [dateTime(item.updatedAt)];
    if (item.code) {
      parts.push(item.code);
    }
    if (item.hasTenderCode) {
      parts.push(t("today.tagTender"));
    }
    return parts.filter(Boolean).join(" \u00b7 ");
  }

  function openDetail(index) {
    var item = state.items[index];
    if (!refs || !item) {
      return;
    }

    state.detailIndex = index;
    state.lastFocus = document.activeElement;

    refs.detailTitle.textContent = item.title || t("today.noTitle");
    refs.detailKicker.textContent = detailMeta(item);
    syncActionButtons();

    refs.scrim.hidden = false;
    refs.detail.hidden = false;

    if (typeof refs.detailClose.focus === "function") {
      refs.detailClose.focus();
    }
  }

  function closeDetail() {
    if (!refs || refs.detail.hidden) {
      return;
    }
    var returnTo = state.lastFocus;

    state.detailIndex = -1;
    state.lastFocus = null;
    refs.detail.hidden = true;
    refs.scrim.hidden = true;

    if (returnTo && typeof returnTo.focus === "function") {
      try {
        returnTo.focus({ preventScroll: true });
      } catch (err) {
        returnTo.focus();
      }
    }
  }

  function openItemLink(index) {
    var item = state.items[index];
    if (!item || !item.url) {
      return;
    }
    app.openLink(item.url);
  }

  function indexFrom(node) {
    var index = parseInt(node.getAttribute("data-index"), 10);
    if (isNaN(index) || index < 0 || index >= state.items.length) {
      return -1;
    }
    return index;
  }

  /* Conversion ----------------------------------------------------------- */

  function conversionKey(url, format) {
    return format + "\u0000" + url;
  }

  function isConverting(url, format) {
    return Boolean(url) && Boolean(converting[conversionKey(url, format)]);
  }

  /*
   * Applies the in-flight set to every rendered action button. Both the list
   * and the sheet are covered: a card may be re-rendered by pagination or a
   * search while its conversion is still running, and the sheet converts the
   * notice it is showing.
   */
  function syncActionButtons() {
    if (!refs) {
      return;
    }

    var buttons = refs.view.querySelectorAll(
      'button[data-action="convert"], button[data-action="open-link"], button[data-action="open-notice"]'
    );
    for (var i = 0; i < buttons.length; i++) {
      var button = buttons[i];
      // The sheet's buttons act on the notice the sheet is showing, never on a
      // list index that a re-render may have moved.
      var inSheet =
        button.getAttribute("data-scope") === "detail" ||
        button.getAttribute("data-action") === "open-notice";
      var item = inSheet
        ? state.items[state.detailIndex]
        : state.items[parseInt(button.getAttribute("data-index"), 10)];

      var blocked = !item || !item.url;
      if (!blocked && button.getAttribute("data-action") === "convert") {
        blocked = isConverting(item.url, button.getAttribute("data-format"));
      }
      button.disabled = blocked;
    }
  }

  // Same markup as app.toast, but with the lifetime the conversion flow asks
  // for. Returns the node so the pending prompt can be retired early.
  function timedToast(message, kind, ttl) {
    var container = document.getElementById("toasts") || document.body;
    var el = element("div", "toast is-" + kind);
    el.setAttribute("role", "status");
    el.textContent = message;
    container.appendChild(el);

    global.setTimeout(function () {
      if (el.parentNode) {
        el.parentNode.removeChild(el);
      }
    }, ttl);

    return el;
  }

  function retireToast(el) {
    if (el && el.parentNode) {
      el.parentNode.removeChild(el);
    }
  }

  function conversionErrorText(err) {
    var status = err && err.status;
    if (status === 429) {
      return t("app.rateLimited");
    }
    if (status === 403) {
      return t("today.convertUnavailable");
    }
    return t("today.convertFailed");
  }

  /*
   * Submits one conversion. The bot sends the file to the chat, so there is
   * nothing to poll: the app acknowledges the submission and then forgets it.
   * The button is disabled for the whole flight, and the pair (notice, format)
   * is de-duplicated so a double tap cannot submit twice.
   */
  function startConversion(index, format) {
    var item = state.items[index];
    if (!item || !item.url || !CONVERT_FORMATS[format] || !global.MagnetAPI) {
      return;
    }

    var key = conversionKey(item.url, format);
    if (converting[key]) {
      return;
    }

    var meta = CONVERT_FORMATS[format];
    converting[key] = true;
    syncActionButtons();

    var pending = timedToast(t("today.converting"), "info", CONVERT_PENDING_TOAST_MS);

    var request;
    try {
      request = global.MagnetAPI.convert(item.url, format);
    } catch (err) {
      finishConversion(key, pending);
      app.toast(conversionErrorText(err), "error");
      return;
    }

    request.then(
      function () {
        finishConversion(key, pending);
        timedToast(meta.label + " " + t("today.sent"), "success", CONVERT_DONE_TOAST_MS);
      },
      function (err) {
        finishConversion(key, pending);
        app.toast(conversionErrorText(err), "error");
      }
    );
  }

  function finishConversion(key, pendingToast) {
    delete converting[key];
    syncActionButtons();
    retireToast(pendingToast);
  }

  /* Events --------------------------------------------------------------- */

  // Walks up to the nearest element carrying `attribute`, stopping at the view
  // root so a stray attribute elsewhere in the shell is never honoured.
  function closest(node, attribute) {
    var current = node;
    while (current && current !== refs.view) {
      if (current.nodeType === 1 && current.getAttribute(attribute)) {
        return current;
      }
      current = current.parentNode;
    }
    return null;
  }

  function onClick(event) {
    if (!refs) {
      return;
    }

    var action = closest(event.target, "data-action");
    if (!action) {
      return;
    }

    var name = action.getAttribute("data-action");

    if (name === "range") {
      setRange(action.getAttribute("data-range"));
      return;
    }
    if (name === "load-more") {
      loadMore();
      return;
    }
    if (name === "retry") {
      reload();
      return;
    }
    if (name === "detail") {
      openDetail(indexFrom(action));
      return;
    }
    if (name === "open-link") {
      openItemLink(indexFrom(action));
      return;
    }
    if (name === "open-notice") {
      openItemLink(state.detailIndex);
      return;
    }
    if (name === "convert") {
      // The sheet has no per-card index: it always converts the notice it is
      // showing.
      var noticeIndex =
        action.getAttribute("data-scope") === "detail" ? state.detailIndex : indexFrom(action);
      startConversion(noticeIndex, action.getAttribute("data-format"));
      return;
    }
    if (name === "close-detail") {
      closeDetail();
    }
  }

  function onKeydown(event) {
    var key = event.key || "";
    if (key !== "Escape" && key !== "Esc") {
      return;
    }
    if (!refs || refs.detail.hidden) {
      return;
    }
    event.preventDefault();
    closeDetail();
  }

  /* Language ------------------------------------------------------------- */

  function syncRowText() {
    var rows = refs.list.querySelectorAll("article.entry");
    for (var i = 0; i < rows.length; i++) {
      var row = rows[i];
      var index = parseInt(row.getAttribute("data-index"), 10);
      var item = state.items[index];
      if (!item) {
        continue;
      }

      var lead = row.querySelector(".entry-lead");
      if (lead) {
        lead.textContent = item.title || t("today.noTitle");
      }
      setText(row, '[data-meta="time"]', dateTime(item.updatedAt));
      setText(row, '[data-meta="code"]', item.code);
      setText(row, '[data-meta="tag-label"]', item.hasTenderCode ? t("today.tagTender") : "");
    }
  }

  function syncDetailText() {
    var item = state.items[state.detailIndex];
    if (!item) {
      return;
    }
    refs.detailTitle.textContent = item.title || t("today.noTitle");
    refs.detailKicker.textContent = detailMeta(item);
  }

  function setText(root, selector, value) {
    var node = root.querySelector(selector);
    if (node) {
      node.textContent = value;
    }
  }

  // Rewrites only the text this view owns; the shell has already applied the
  // static data-i18n nodes, and nothing here rebuilds the list.
  function onLang() {
    if (!refs) {
      return;
    }
    renderRunhead();
    renderStats();
    renderStates();
    renderMore();
    syncRowText();
    syncDetailText();
  }

  /* View lifecycle ------------------------------------------------------- */

  function needsReload() {
    if (!state.loaded || state.failed) {
      return true;
    }
    // core.js clears the flag just before calling mount(), so this normally
    // reads false; it still covers an explicit invalidate that lands between
    // two renders (and a future core that stops clearing the flag first).
    return typeof app.state.isStale === "function" && app.state.isStale(VIEW);
  }

  /**
   * Mount is called on every route activation and must be cheap when nothing
   * changed: re-render from memory, and only hit /api/notices when this view
   * still owes a first page, the last attempt failed, or it was invalidated.
   */
  function mount() {
    if (!ensureDOM()) {
      return;
    }

    syncSearchValue();
    syncRangeButtons();

    // Returning to the tab with a sheet open should show the list, not a stale
    // dialog over a fresh render.
    closeDetail();

    if (!app.state || !app.state.initData) {
      // registerView() mounts once before core.init() has authenticated; that
      // call may only draw the skeleton.
      showSkeleton();
      return;
    }

    // A write elsewhere (a new keyword changes the counters) marks the view
    // stale; the strip must refetch even when the notice list does not.
    if (needsReload()) {
      stats.loaded = false;
    }
    loadStatistics();

    if (needsReload()) {
      reload();
      return;
    }

    renderAll();
  }

  app.registerView(VIEW, { mount: mount, onLang: onLang });
})(window);
