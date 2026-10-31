/*
 * alarms.js — the read-only Alarms view.
 *
 * Two independent blocks share one section:
 *   1. "Alarm keywords" — the user's alarm keywords, id/name/created on.
 *   2. "Alarm records"  — paged alert history with a debounced server-side
 *      search and a deep link back into the bot for each row.
 *
 * Each block keeps its own loading/empty/error state so a slow or failing
 * records request never blanks the keyword list above it. Records page with the
 * API's nextOffset cursor; "Load more" disappears once the cursor is null.
 *
 * P2 is read-only: add/edit/delete are rendered but disabled, matching the
 * layout the write phase will need.
 */
(function (global) {
  "use strict";

  var core = global.MagnetApp;
  if (!core) {
    // index.html loads js/core.js first; without it there is no transport and no
    // view registry, so this script must not pretend to render anything.
    return;
  }

  var VIEW = "alarms";
  var SECTION_ID = "view-alarms";
  var RECORD_PAGE_SIZE = 20;
  var RECORD_SEARCH_DEBOUNCE_MS = 300;
  // Only Telegram deep links are opened from a record row. Anything else (a
  // compromised API, a future field with a different meaning) is dropped
  // instead of being handed to openLink.
  var TELEGRAM_LINK_PREFIX = "https://t.me/";

  var els = null;

  var keywords = { items: [], status: "idle", loaded: false };
  var records = {
    items: [],
    q: "",
    nextOffset: null,
    status: "idle",
    loaded: false,
    // Monotonic request token: a slower response from a superseded search must
    // not overwrite the newer result.
    seq: 0,
  };

  var TEMPLATE =
    '<section class="alarms-block" data-block="alarm-keywords">' +
    '<div class="block-head">' +
    '<h2 class="block-title">Alarm keywords</h2>' +
    '<button type="button" class="btn block-add" disabled title="Available in the next release">' +
    "+ Add alarm keyword</button>" +
    "</div>" +
    '<div class="skeleton" data-slot="loading">Loading\u2026</div>' +
    '<div class="error" data-slot="error" role="alert" hidden></div>' +
    '<div class="empty" data-slot="empty" hidden></div>' +
    '<ul class="alarm-kw-list" data-slot="list" hidden></ul>' +
    "</section>" +
    '<section class="alarms-block" data-block="alarm-records">' +
    '<div class="block-head"><h2 class="block-title">Alarm records</h2></div>' +
    '<input class="search-input" type="search" data-role="record-search" placeholder="Search alarm records" ' +
    'aria-label="Search alarm records" autocomplete="off" />' +
    '<div class="skeleton" data-slot="loading">Loading\u2026</div>' +
    '<div class="error" data-slot="error" role="alert" hidden></div>' +
    '<div class="empty" data-slot="empty" hidden></div>' +
    '<ul class="record-list" data-slot="list" hidden></ul>' +
    '<button type="button" class="btn-ghost load-more" data-role="load-more" hidden>Load more</button>' +
    "</section>";

  function api() {
    var apiNamespace = global.MagnetAPI;
    if (!apiNamespace) {
      throw new Error("MagnetAPI is not loaded");
    }
    return apiNamespace;
  }

  /* Helpers -------------------------------------------------------------- */

  function pad2(value) {
    return value < 10 ? "0" + value : String(value);
  }

  // The API returns RFC3339 UTC instants; a plain YYYY-MM-DD is passed through
  // so a date-only value cannot shift a day in a negative UTC offset.
  function formatDate(value) {
    var raw = value == null ? "" : String(value);
    if (!raw) {
      return "\u2014";
    }

    if (/^\d{4}-\d{2}-\d{2}$/.test(raw)) {
      return raw;
    }

    var parsed = new Date(raw);
    if (isNaN(parsed.getTime())) {
      var prefix = raw.slice(0, 10);
      return /^\d{4}-\d{2}-\d{2}$/.test(prefix) ? prefix : "\u2014";
    }

    return (
      parsed.getFullYear() + "-" + pad2(parsed.getMonth() + 1) + "-" + pad2(parsed.getDate())
    );
  }

  function safeTelegramLink(value) {
    var raw = value == null ? "" : String(value).trim();
    if (raw.indexOf(TELEGRAM_LINK_PREFIX) !== 0) {
      return "";
    }
    return raw;
  }

  function normalizeKeywords(payload) {
    var raw = payload && Array.isArray(payload.items) ? payload.items : [];
    var result = [];

    for (var i = 0; i < raw.length; i++) {
      var item = raw[i];
      if (!item || typeof item !== "object") {
        continue;
      }
      result.push({
        id: item.id,
        keyword: typeof item.keyword === "string" ? item.keyword : "",
        createdAt: item.createdAt,
      });
    }

    return result;
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
        businessId: typeof item.businessId === "string" ? item.businessId : "",
        creditName: typeof item.creditName === "string" ? item.creditName : "",
        startDate: item.startDate,
        link: safeTelegramLink(item.link),
      });
    }

    return result;
  }

  function nextOffsetOf(payload) {
    var value = payload && payload.nextOffset;
    return typeof value === "number" && isFinite(value) && value >= 0 ? value : null;
  }

  /* Rendering ------------------------------------------------------------ */

  function actionButton(label, glyph, disabledTitle) {
    return (
      '<button type="button" class="icon-btn" disabled title="' +
      core.escapeHTML(disabledTitle || "Available in the next release") +
      '" aria-label="' +
      core.escapeHTML(label) +
      '">' +
      glyph +
      "</button>"
    );
  }

  function alarmKeywordRow(item) {
    var esc = core.escapeHTML;

    return (
      '<li class="alarm-kw-row">' +
      '<span class="alarm-kw-id">#' +
      esc(String(item.id)) +
      "</span>" +
      '<span class="alarm-kw-name" title="' +
      esc(item.keyword) +
      '">' +
      esc(item.keyword) +
      "</span>" +
      '<span class="alarm-kw-date">' +
      esc(formatDate(item.createdAt)) +
      "</span>" +
      '<span class="alarm-kw-actions">' +
      actionButton("Edit alarm keyword", "\u270f\ufe0f") +
      actionButton("Delete alarm keyword", "\ud83d\uddd1") +
      "</span>" +
      "</li>"
    );
  }

  function recordLinkButton(link) {
    var label = "Open alarm record in Telegram";

    if (!link) {
      return actionButton(label, "\u2197", "No link available");
    }

    return (
      '<button type="button" class="icon-btn" data-link="' +
      core.escapeHTML(link) +
      '" title="Open in Telegram" aria-label="' +
      core.escapeHTML(label) +
      '">\u2197</button>'
    );
  }

  function recordRow(item) {
    var esc = core.escapeHTML;
    var name = item.creditName || "Unknown";

    return (
      '<li class="record-row">' +
      '<span class="record-text">' +
      '<span class="record-name" title="' +
      esc(item.creditName) +
      '">' +
      esc(name) +
      "</span>" +
      '<span class="record-date">' +
      esc(formatDate(item.startDate)) +
      "</span>" +
      "</span>" +
      recordLinkButton(item.link) +
      "</li>"
    );
  }

  function renderRetry(container, message, retry) {
    container.textContent = "";

    var text = document.createElement("span");
    text.className = "error-text";
    text.textContent = message;

    var actions = document.createElement("div");
    actions.className = "error-actions";

    var button = document.createElement("button");
    button.type = "button";
    button.className = "btn-ghost retry-btn";
    button.textContent = "Retry";
    button.addEventListener("click", retry);

    actions.appendChild(button);
    container.appendChild(text);
    container.appendChild(actions);
  }

  function renderKeywordBlock() {
    var state = keywords;
    var showList = state.status === "ready" && state.items.length > 0;
    var showEmpty = state.status === "ready" && state.items.length === 0;

    els.kwLoading.hidden = state.status !== "loading";
    els.kwError.hidden = state.status !== "error";
    els.kwEmpty.hidden = !showEmpty;
    els.kwList.hidden = !showList;

    if (state.status === "error") {
      renderRetry(els.kwError, "Could not load alarm keywords.", function () {
        loadKeywords();
      });
    }

    if (showEmpty) {
      els.kwEmpty.textContent =
        "No alarm keywords yet. Add alarm keywords from the bot, then reopen this page.";
    }

    if (showList) {
      els.kwList.innerHTML = state.items.map(alarmKeywordRow).join("");
    }
  }

  function renderRecordBlock() {
    var state = records;
    var showSkeleton = state.status === "loading" && state.items.length === 0;
    var showEmpty = state.status === "ready" && state.items.length === 0;

    els.recLoading.hidden = !showSkeleton;
    els.recError.hidden = state.status !== "error";
    els.recEmpty.hidden = !showEmpty;
    els.recList.hidden = state.items.length === 0;

    // Keep the cursor button in place while a page is in flight (disabled) so
    // the list height does not jump; hide it only when the cursor is exhausted.
    var hasCursor = state.nextOffset !== null;
    els.loadMore.hidden = !hasCursor || state.items.length === 0;
    els.loadMore.disabled = state.status === "loading" || state.status === "error";

    if (state.status === "error") {
      renderRetry(els.recError, "Could not load alarm records.", retryRecords);
    }

    if (showEmpty) {
      els.recEmpty.textContent = state.q
        ? 'No alarm records match "' + state.q + '".'
        : "No alarm records yet.";
    }

    if (state.items.length > 0) {
      els.recList.innerHTML = state.items.map(recordRow).join("");
    }
  }

  /* Data ----------------------------------------------------------------- */

  function loadKeywords() {
    if (keywords.status === "loading") {
      return Promise.resolve();
    }

    keywords.status = "loading";
    renderKeywordBlock();

    return Promise.resolve()
      .then(function () {
        return api().alarms();
      })
      .then(function (payload) {
        keywords.items = normalizeKeywords(payload);
        keywords.loaded = true;
        keywords.status = "ready";
        renderKeywordBlock();
      })
      .catch(function () {
        keywords.status = "error";
        renderKeywordBlock();
      });
  }

  function retryRecords() {
    loadRecords(records.items.length === 0);
  }

  function loadRecords(reset) {
    var state = records;

    if (!reset && state.nextOffset === null) {
      return Promise.resolve();
    }

    var offset = reset ? 0 : state.nextOffset;
    var seq = ++state.seq;

    if (reset) {
      state.items = [];
      state.nextOffset = null;
    }

    state.status = "loading";
    renderRecordBlock();

    return Promise.resolve()
      .then(function () {
        return api().alarmRecords({ q: state.q, offset: offset, limit: RECORD_PAGE_SIZE });
      })
      .then(function (payload) {
        // A newer search already replaced this request; its response wins.
        if (seq !== state.seq) {
          return;
        }

        var page = normalizeRecords(payload);
        state.items = reset ? page : state.items.concat(page);
        state.nextOffset = nextOffsetOf(payload);
        state.loaded = true;
        state.status = "ready";
        renderRecordBlock();
      })
      .catch(function () {
        if (seq !== state.seq) {
          return;
        }

        state.status = "error";
        renderRecordBlock();
      });
  }

  /* Events --------------------------------------------------------------- */

  function onRecordClick(event) {
    var target = event.target;
    var button =
      target && typeof target.closest === "function" ? target.closest("button[data-link]") : null;
    if (!button) {
      return;
    }

    var link = button.getAttribute("data-link");
    if (link) {
      core.openLink(link);
    }
  }

  /* View ---------------------------------------------------------------- */

  function slot(root, name) {
    return root.querySelector('[data-slot="' + name + '"]');
  }

  function build(section) {
    section.innerHTML = TEMPLATE;

    var keywordBlock = section.querySelector('[data-block="alarm-keywords"]');
    var recordBlock = section.querySelector('[data-block="alarm-records"]');

    return {
      kwLoading: slot(keywordBlock, "loading"),
      kwError: slot(keywordBlock, "error"),
      kwEmpty: slot(keywordBlock, "empty"),
      kwList: slot(keywordBlock, "list"),
      recLoading: slot(recordBlock, "loading"),
      recError: slot(recordBlock, "error"),
      recEmpty: slot(recordBlock, "empty"),
      recList: slot(recordBlock, "list"),
      search: section.querySelector('[data-role="record-search"]'),
      loadMore: section.querySelector('[data-role="load-more"]'),
    };
  }

  function bind() {
    // Debounced so typing does not issue one request per keystroke; the trailing
    // call reads the final value, so intermediate keystrokes are simply skipped.
    var runSearch = core.debounce(function () {
      records.q = els.search.value.trim();
      loadRecords(true);
    }, RECORD_SEARCH_DEBOUNCE_MS);

    els.search.addEventListener("input", runSearch);
    els.recList.addEventListener("click", onRecordClick);
    els.loadMore.addEventListener("click", function () {
      if (records.status === "ready") {
        loadRecords(false);
      }
    });
  }

  function mount() {
    var section = document.getElementById(SECTION_ID);
    if (!section) {
      return;
    }

    if (!els) {
      els = build(section);
      bind();
    }

    // The two blocks load independently: a failure in one must not block the
    // other, and each owns its skeleton.
    if (!keywords.loaded || core.state.isStale(VIEW)) {
      loadKeywords();
    }
    if (!records.loaded || core.state.isStale(VIEW)) {
      return loadRecords(true);
    }
  }

  core.registerView(VIEW, { mount: mount });
})(window);
