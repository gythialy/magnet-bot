/*
 * today.js — the Today view.
 *
 * Owns #view-today: the statistics bar, the search box, the date-range chips,
 * the notice list, the "Load more" pager and the notice detail sheet.
 *
 * The document only ships three placeholders inside the section
 * (data-slot="loading" / "empty" / "error"); every other node is built here on
 * the first mount and reused afterwards. index.html is owned by the shell and
 * must not need a change every time this view grows.
 *
 * Behaviour notes that are easy to get wrong later:
 *   - core.js calls mount() once when this script registers (before init() has
 *     run, so before initData exists) and again from route(). The first call
 *     only draws the skeleton; the second one fetches.
 *   - core.js deletes the "stale" flag *before* calling mount(), so mount() has
 *     to decide on its own whether it owes a request. It fetches when nothing
 *     has loaded yet, when the previous attempt failed, or when someone
 *     invalidated the view between renders.
 *   - Titles, project codes, the search text and any server date string are
 *     user data: they are escaped before being interpolated into innerHTML.
 *     URLs are never interpolated at all — a card carries an index and the
 *     link is read back from state, and only https:// links are kept.
 *
 * No build step, no third-party code, no external requests.
 */
(function () {
  "use strict";

  var app = window.MagnetApp;
  if (!app || typeof app.registerView !== "function") {
    // Without core.js there is no shell to register against; refusing here is
    // better than half-rendering a view the router will never mount.
    return;
  }

  var RANGES = ["today", "7d", "all"];
  var PAGE_LIMIT = 20;
  var SEARCH_DEBOUNCE_MS = 300;

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
  var GROUP_SEPARATOR = /\B(?=(\d{3})+(?!\d))/g;

  var RANGE_EMPTY_TEXT = {
    today: "No notices today.",
    "7d": "No notices in the last 7 days.",
    all: "No notices yet.",
  };

  // Nodes, queried or created once. null until ensureDOM() succeeds.
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
    loading: false,
    loaded: false,
    failed: false,
    errorMessage: "",
    detailIndex: -1,
  };

  // Bumped per request so a slow response for an old query can never overwrite
  // the list for the query the user is actually looking at.
  var requestSeq = 0;

  var stats = {
    history: null,
    keywords: null,
    related: null,
    alarms: null,
    loaded: false,
    loading: false,
  };

  /* Small helpers --------------------------------------------------------- */

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

  // Thousands separators without Intl: the grouping must not depend on the
  // client locale, and every target webview has String.replace.
  function formatCount(count) {
    if (count === null || count === undefined) {
      return "—";
    }
    return String(count).replace(GROUP_SEPARATOR, ",");
  }

  function pad2(number) {
    return number < 10 ? "0" + number : String(number);
  }

  // The API sends RFC3339 in UTC; the list shows the phone's local wall clock
  // as YYYY-MM-DD HH:mm, which is what the user compares against the notice.
  function formatLocalTime(value) {
    var raw = String(value == null ? "" : value);
    if (!raw) {
      return "";
    }

    var date = new Date(raw);
    if (isNaN(date.getTime())) {
      return "";
    }

    return (
      date.getFullYear() +
      "-" +
      pad2(date.getMonth() + 1) +
      "-" +
      pad2(date.getDate()) +
      " " +
      pad2(date.getHours()) +
      ":" +
      pad2(date.getMinutes())
    );
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

  /* Model ---------------------------------------------------------------- */

  function normalizeItem(raw) {
    if (!raw || typeof raw !== "object") {
      return null;
    }

    var title = typeof raw.title === "string" ? raw.title : "";
    var updatedAt = typeof raw.updatedAt === "string" ? raw.updatedAt : "";

    return {
      url: safeURL(raw.url),
      title: title,
      code: extractProjectCode(title),
      time: formatLocalTime(updatedAt),
      // The contract says boolean; tolerate the numeric form a stale page of
      // JSON might still carry.
      hasTenderCode: raw.hasTenderCode === true || raw.hasTenderCode === 1,
    };
  }

  function itemKey(item) {
    return item.url || item.title + "\u0000" + item.time;
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

  function metaHTML(item) {
    var parts = [];
    if (item.code) {
      parts.push('<span class="today-code">' + escape(item.code) + "</span>");
    }
    if (item.time) {
      parts.push('<span class="today-time">' + escape(item.time) + "</span>");
    }
    return parts.join(' <span class="today-sep" aria-hidden="true">·</span> ');
  }

  /* DOM construction ----------------------------------------------------- */

  function slot(root, name) {
    var existing = root.querySelector('[data-slot="' + name + '"]');
    if (existing) {
      return existing;
    }
    // index.html ships all three placeholders; the fallback keeps this view
    // working if the shell ever trims them.
    var created = element("div", name === "loading" ? "skeleton" : name);
    created.setAttribute("data-slot", name);
    created.hidden = name !== "loading";
    if (name === "loading") {
      created.textContent = "Loading…";
    }
    root.appendChild(created);
    return created;
  }

  function statHTML(key, label) {
    return (
      '<div class="today-stat">' +
      '<span class="today-stat-value" data-stat="' + key + '">—</span>' +
      '<span class="today-stat-label">' + label + "</span>" +
      "</div>"
    );
  }

  function toolbarHTML() {
    return (
      '<div class="today-stats">' +
      statHTML("history", "History") +
      statHTML("keywords", "Keywords") +
      statHTML("related", "Related notices") +
      statHTML("alarms", "Alarm keywords") +
      "</div>" +
      '<div class="today-search">' +
      '<input class="today-search-input" type="search" data-slot="search"' +
      ' placeholder="Search title or project code"' +
      ' autocomplete="off" autocapitalize="off" spellcheck="false"' +
      ' enterkeyhint="search" aria-label="Search title or project code" />' +
      "</div>" +
      '<div class="chip-row today-ranges" role="group" aria-label="Date range">' +
      '<button class="chip is-active" type="button" data-action="range"' +
      ' data-range="today" aria-pressed="true">Today</button>' +
      '<button class="chip" type="button" data-action="range"' +
      ' data-range="7d" aria-pressed="false">7 days</button>' +
      '<button class="chip" type="button" data-action="range"' +
      ' data-range="all" aria-pressed="false">All</button>' +
      "</div>"
    );
  }

  function detailHTML() {
    return (
      '<div class="modal today-modal" role="dialog" aria-modal="true"' +
      ' aria-labelledby="today-detail-title">' +
      '<div class="today-modal-head">' +
      '<h2 class="today-modal-title" id="today-detail-title" data-slot="detail-title"></h2>' +
      '<button class="modal-close" type="button" data-action="close-detail"' +
      ' aria-label="Close details">✕</button>' +
      "</div>" +
      '<p class="today-modal-meta" data-slot="detail-meta"></p>' +
      '<div class="today-modal-actions">' +
      '<button class="btn today-modal-open" type="button" data-action="open-notice">' +
      "Open notice page</button>" +
      "</div>" +
      "</div>"
    );
  }

  function ensureDOM() {
    var view = document.getElementById("view-today");
    if (!view) {
      return null;
    }
    if (refs && refs.view === view) {
      return refs;
    }

    var loading = slot(view, "loading");
    var empty = slot(view, "empty");
    var error = slot(view, "error");

    var toolbar = element("div", "today-toolbar");
    toolbar.innerHTML = toolbarHTML();

    var list = element("div", "today-list");
    list.setAttribute("data-slot", "list");

    var moreWrap = element("div", "today-more-wrap");
    moreWrap.hidden = true;
    moreWrap.innerHTML =
      '<button class="btn-ghost today-more" type="button" data-action="load-more">Load more</button>';

    // Built before the placeholders so the static states keep their order in
    // the document while the list grows above them.
    view.insertBefore(toolbar, loading);
    view.insertBefore(list, loading);
    view.insertBefore(moreWrap, loading);

    // The sheet lives inside the section on purpose: #view-today[hidden] hides
    // its subtree, so switching tabs cannot leave a stale dialog on screen.
    var detail = element("div", "modal-backdrop");
    detail.hidden = true;
    detail.setAttribute("data-slot", "detail");
    detail.innerHTML = detailHTML();
    view.appendChild(detail);

    var statNodes = {};
    Array.prototype.forEach.call(toolbar.querySelectorAll("[data-stat]"), function (node) {
      statNodes[node.getAttribute("data-stat")] = node;
    });

    refs = {
      view: view,
      toolbar: toolbar,
      stats: statNodes,
      search: toolbar.querySelector('[data-slot="search"]'),
      rangeChips: toolbar.querySelectorAll("[data-action='range']"),
      list: list,
      moreWrap: moreWrap,
      more: moreWrap.querySelector("[data-action='load-more']"),
      loading: loading,
      empty: empty,
      error: error,
      detail: detail,
      detailTitle: detail.querySelector('[data-slot="detail-title"]'),
      detailMeta: detail.querySelector('[data-slot="detail-meta"]'),
      detailOpen: detail.querySelector("[data-action='open-notice']"),
      detailClose: detail.querySelector("[data-action='close-detail']"),
    };

    refs.search.addEventListener("input", app.debounce(applySearch, SEARCH_DEBOUNCE_MS));
    view.addEventListener("click", onClick);
    if (!escapeWired) {
      document.addEventListener("keydown", onKeydown);
      escapeWired = true;
    }

    syncSearchValue();
    syncRangeChips();
    renderStats();

    return refs;
  }

  /* Rendering ------------------------------------------------------------ */

  function cardHTML(item, index) {
    var title = item.title || "(no title)";
    var meta = metaHTML(item);
    var flame = item.hasTenderCode
      ? '<span class="today-flame" role="img" aria-label="Has tender code"' +
        ' title="Has tender code">🔥</span>'
      : "";
    // The action is only offered when there is somewhere to go; the card body
    // still opens the detail sheet either way.
    var open = item.url
      ? '<button class="today-open" type="button" data-action="open-link"' +
        ' data-index="' + index + '" aria-label="Open notice page">↗ Open</button>'
      : "";

    // The article carries the detail action as well so a tap on the card's own
    // padding still opens the sheet; the inner button is what supplies the
    // keyboard/AT entry point, and "Open" sits closer to the tap than either of
    // them, so launching a link never opens the sheet by accident.
    return (
      '<article class="card today-card" data-action="detail" data-index="' + index + '">' +
      '<div class="today-card-row">' +
      '<button class="today-card-body" type="button" data-action="detail"' +
      ' data-index="' + index + '" aria-haspopup="dialog">' +
      '<span class="today-card-head">' +
      '<span class="card-title today-card-title">' + escape(title) + "</span>" +
      flame +
      "</span>" +
      (meta ? '<span class="card-meta today-card-meta">' + meta + "</span>" : "") +
      "</button>" +
      (open ? '<div class="today-card-side">' + open + "</div>" : "") +
      "</div>" +
      "</article>"
    );
  }

  function renderList() {
    if (!refs) {
      return;
    }

    var html = "";
    for (var i = 0; i < state.items.length; i++) {
      html += cardHTML(state.items[i], i);
    }
    refs.list.innerHTML = html;
  }

  function renderStates() {
    if (!refs) {
      return;
    }

    var firstLoad = state.loading && state.items.length === 0;
    var failed = !state.loading && state.failed && state.items.length === 0;
    var empty = !state.loading && !state.failed && state.loaded && state.items.length === 0;

    refs.loading.hidden = !firstLoad;
    refs.error.hidden = !failed;
    refs.empty.hidden = !empty;

    if (failed) {
      renderError();
    }
    if (empty) {
      renderEmpty();
    }
  }

  function renderError() {
    var message = state.errorMessage || "Couldn't load notices.";
    refs.error.innerHTML =
      "<span>" + escape(message) + "</span>" +
      '<button class="btn-ghost today-retry" type="button" data-action="retry">Retry</button>';
  }

  function renderEmpty() {
    var message;
    if (state.q) {
      message = 'No notices match "' + escape(state.q) + '".';
    } else {
      message = RANGE_EMPTY_TEXT[state.range] || RANGE_EMPTY_TEXT.today;
    }
    refs.empty.innerHTML = message;
  }

  function renderMore() {
    if (!refs) {
      return;
    }

    var hasMore = state.nextOffset !== null;
    refs.moreWrap.hidden = !hasMore;
    refs.more.disabled = state.loading;
    refs.more.setAttribute("aria-busy", state.loading ? "true" : "false");
    refs.more.textContent = state.loading ? "Loading…" : "Load more";
  }

  function renderStats() {
    if (!refs) {
      return;
    }
    // Counts are server integers, not user text, so textContent is used here:
    // it cannot be parsed as markup no matter what the API returns.
    refs.stats.history.textContent = formatCount(stats.history);
    refs.stats.keywords.textContent = formatCount(stats.keywords);
    refs.stats.related.textContent = formatCount(stats.related);
    refs.stats.alarms.textContent = formatCount(stats.alarms);
  }

  function renderAll() {
    renderStats();
    renderList();
    renderStates();
    renderMore();
  }

  function showLoadingOnly() {
    if (!refs) {
      return;
    }
    refs.loading.hidden = false;
    refs.empty.hidden = true;
    refs.error.hidden = true;
    refs.moreWrap.hidden = true;
  }

  function syncRangeChips() {
    if (!refs) {
      return;
    }
    Array.prototype.forEach.call(refs.rangeChips, function (chip) {
      var active = chip.getAttribute("data-range") === state.range;
      chip.classList.toggle("is-active", active);
      chip.setAttribute("aria-pressed", active ? "true" : "false");
    });
  }

  function syncSearchValue() {
    if (refs && refs.search.value !== state.q) {
      refs.search.value = state.q;
    }
  }

  /* Statistics ----------------------------------------------------------- */

  function loadStatistics() {
    if (!refs || stats.loaded || stats.loading || !window.MagnetAPI) {
      return;
    }

    stats.loading = true;

    window.MagnetAPI.statistics()
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
    }
    mergeItems(incoming);

    state.loaded = true;
    state.failed = false;
    state.errorMessage = "";

    var next = payload ? payload.nextOffset : null;
    // Only accept a cursor that actually moves forward, and never offer a
    // "Load more" that would fetch the page we just rendered again.
    if (typeof next === "number" && isFinite(next) && next > offset && incoming.length > 0) {
      state.nextOffset = next;
    } else {
      state.nextOffset = null;
    }
  }

  function errorText(err) {
    var detail = err && err.message ? String(err.message) : "";
    return detail ? "Couldn't load notices: " + detail : "Couldn't load notices.";
  }

  function fetchPage(append) {
    if (!refs) {
      return Promise.resolve();
    }
    if (!app.state || !app.state.initData || !window.MagnetAPI) {
      // init() has not run yet; the router mounts this view again once it has,
      // and that mount is the one allowed to hit the network.
      showLoadingOnly();
      return Promise.resolve();
    }

    var sequence = ++requestSeq;
    var offset = append && typeof state.nextOffset === "number" ? state.nextOffset : 0;

    state.loading = true;
    state.failed = false;
    renderStates();
    renderMore();

    return window.MagnetAPI.notices({
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
          app.toast("Couldn't load more notices.", "error");
          return;
        }

        state.loaded = false;
        state.failed = true;
        state.errorMessage = errorText(err);
        renderStates();
        renderMore();
      });
  }

  function reload() {
    if (!refs) {
      return Promise.resolve();
    }

    // Clear first: a spinner over the previous filter's rows would claim those
    // rows belong to the range or query that is loading now.
    state.items = [];
    state.seen = Object.create(null);
    state.nextOffset = null;
    state.loaded = false;
    state.failed = false;
    state.errorMessage = "";

    closeDetail();
    renderList();
    renderStates();
    renderMore();

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
    syncRangeChips();
    reload();
  }

  /* Detail sheet --------------------------------------------------------- */

  function openDetail(index) {
    var item = state.items[index];
    if (!refs || !item) {
      return;
    }

    state.detailIndex = index;

    var meta = metaHTML(item);
    refs.detailTitle.innerHTML = escape(item.title || "(no title)");
    refs.detailMeta.innerHTML = meta;
    refs.detailMeta.hidden = !meta;
    refs.detailOpen.disabled = !item.url;
    refs.detail.hidden = false;

    if (typeof refs.detailClose.focus === "function") {
      refs.detailClose.focus();
    }
  }

  function closeDetail() {
    if (!refs) {
      return;
    }
    state.detailIndex = -1;
    refs.detail.hidden = true;
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
      // A click on the backdrop closes the sheet; a click inside the panel
      // matches no action and stops here instead of closing anything.
      if (!refs.detail.hidden && event.target === refs.detail) {
        closeDetail();
      }
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

  /* View lifecycle ------------------------------------------------------- */

  function needsReload() {
    if (!state.loaded || state.failed) {
      return true;
    }
    // core.js clears the flag just before calling mount(), so this normally
    // reads false; it still covers an explicit invalidate that lands between
    // two renders (and a future core that stops clearing the flag first).
    return typeof app.state.isStale === "function" && app.state.isStale("today");
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
    syncRangeChips();

    // Returning to the tab with a sheet open should show the list, not a stale
    // dialog over a fresh render.
    closeDetail();

    if (!app.state || !app.state.initData) {
      // registerView() mounts once before core.init() has authenticated; that
      // call may only draw the skeleton.
      showLoadingOnly();
      return;
    }

    loadStatistics();

    if (needsReload()) {
      reload();
      return;
    }

    renderAll();
  }

  app.registerView("today", { mount: mount });
})();
