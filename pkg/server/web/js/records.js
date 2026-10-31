/*
 * records.js — the Records view (read only).
 *
 * Alarm hits used to live under Alarms, stacked below the keyword editor, and a
 * row opened the bot chat through a t.me link: the client jumped to the system
 * browser and then back into Telegram just to see one record. They are a
 * browsing surface, not a setting, so they get their own tab and the detail
 * opens as a sheet built from data the page already has.
 *
 * Records page with the API's nextOffset cursor; "Load more" disappears once the
 * cursor is null. A monotonic request token keeps a slow response from a
 * superseded search from overwriting the newer result.
 */
(function (global) {
  "use strict";

  var core = global.MagnetApp;
  if (!core) {
    // index.html loads js/core.js first; without it there is no transport and no
    // view registry, so this script must not pretend to render anything.
    return;
  }

  var VIEW = "records";
  var SECTION_ID = "view-records";
  var PAGE_SIZE = 20;
  var SEARCH_DEBOUNCE_MS = 300;
  // Only Telegram deep links are handed to openTelegramLink. Anything else (a
  // compromised API, a field whose meaning changes later) is dropped rather than
  // passed to a client method that trusts its argument.
  var TELEGRAM_LINK_PREFIX = "https://t.me/";

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
  };
  var sheet = { el: null, title: null, body: null, actions: null, lastFocus: null };

  var TEMPLATE =
    '<div class="column-head">' +
    '<span class="column-head-label">Records</span>' +
    '<span class="column-head-count" data-slot="head-count">\u2014</span>' +
    '<span class="column-head-time" data-slot="head-time"></span>' +
    "</div>" +
    '<div class="records-toolbar">' +
    '<input class="search-input" type="search" data-role="search" ' +
    'placeholder="Search by credit name" aria-label="Search alarm records" autocomplete="off" />' +
    "</div>" +

    '<div class="skeleton" data-slot="loading">Loading\u2026</div>' +
    '<div class="error" data-slot="error" role="alert" hidden></div>' +
    '<div class="empty" data-slot="empty" hidden></div>' +
    '<ul class="record-list" data-slot="list" hidden></ul>' +
    '<button type="button" class="btn-ghost load-more" data-role="load-more" hidden>Load more</button>' +
    '<div class="sheet-backdrop" data-role="sheet" hidden>' +
    '<div class="sheet" role="dialog" aria-modal="true" aria-labelledby="record-sheet-title">' +
    '<div class="sheet-grab" aria-hidden="true"></div>' +
    '<div class="sheet-head">' +
    '<h2 class="sheet-title" id="record-sheet-title" data-slot="sheet-title"></h2>' +
    '<button class="icon-btn" type="button" data-role="sheet-close" aria-label="Close record">' + core.icon("close") + '</button>' +
    "</div>" +
    '<div class="sheet-body" data-slot="sheet-body"></div>' +
    '<div class="sheet-actions" data-slot="sheet-actions"></div>' +
    "</div>" +
    "</div>";

  function api() {
    var apiNamespace = global.MagnetAPI;
    if (!apiNamespace) {
      throw new Error("MagnetAPI is not loaded");
    }
    return apiNamespace;
  }

  function pad2(value) {
    return value < 10 ? "0" + value : String(value);
  }

  // ISO timestamps arrive in UTC; the record dates are calendar days, so they are
  // rendered in the device zone instead of the raw UTC day.
  function formatDate(value) {
    if (!value) {
      return "";
    }

    var parsed = new Date(value);
    if (isNaN(parsed.getTime())) {
      return "";
    }

    return parsed.getFullYear() + "-" + pad2(parsed.getMonth() + 1) + "-" + pad2(parsed.getDate());
  }

  function text(value) {
    return value == null ? "" : String(value).trim();
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
        pageUrl1: text(item.pageUrl1),
        pageUrl2: text(item.pageUrl2),
        noticeId: text(item.noticeId),
        originNoticeId: text(item.originNoticeId),
        link: safeTelegramLink(item.link),
      });
    }

    return result;
  }

  function safeTelegramLink(value) {
    var raw = text(value);
    return raw.indexOf(TELEGRAM_LINK_PREFIX) === 0 ? raw : "";
  }

  function nextOffsetOf(payload) {
    var value = payload && payload.nextOffset;
    return typeof value === "number" && isFinite(value) && value >= 0 ? value : null;
  }

  /* Rendering ------------------------------------------------------------- */

  function recordRow(item) {
    var esc = core.escapeHTML;
    var name = item.creditName || "Unknown credit name";
    var meta = [item.businessId, formatDate(item.startDate)].filter(Boolean).join(" \u00b7 ");

    return (
      '<li class="record-row">' +
      '<button type="button" class="entry record-button" data-id="' +
      esc(item.businessId) +
      '">' +
      '<span class="entry-main">' +
      '<span class="entry-lead">' +
      esc(name) +
      "</span>" +
      '<span class="entry-meta">' +
      esc(meta) +
      "</span>" +
      "</span>" +
      '<span class="record-chevron" aria-hidden="true"></span>' +
      "</button>" +
      "</li>"
    );
  }

  function renderRetry(container, message) {
    container.textContent = "";

    var label = document.createElement("span");
    label.textContent = message;

    var button = document.createElement("button");
    button.type = "button";
    button.className = "btn-ghost";
    button.textContent = "Try again";
    button.addEventListener("click", function () {
      loadRecords(state.items.length === 0);
    });

    container.appendChild(label);
    container.appendChild(button);
    container.hidden = false;
  }

  function render() {
    var showSkeleton = state.status === "loading" && state.items.length === 0;
    var showEmpty = state.status === "ready" && state.items.length === 0;

    els.loading.hidden = !showSkeleton;
    els.empty.hidden = !showEmpty;
    els.list.hidden = state.items.length === 0;

    if (state.status === "error") {
      renderRetry(els.error, "Could not load alarm records.");
    } else {
      els.error.hidden = true;
    }

    var hasCursor = state.nextOffset !== null;
    els.loadMore.hidden = !hasCursor || state.items.length === 0;
    els.loadMore.disabled = state.status === "loading" || state.status === "error";

    renderHead();

    if (showEmpty) {
      els.empty.textContent = state.q
        ? 'No alarm records match "' + state.q + '".'
        : "No alarm records yet. Alarm keywords add them as hits arrive.";
    }

    els.list.innerHTML = state.items.length > 0 ? state.items.map(recordRow).join("") : "";
  }

  /* Detail sheet ---------------------------------------------------------- */

  /* The head carries the count and the clock; the list below changes, this
     strip only fades, so switching a search never reflows the page. */
  function renderHead() {
    if (els.headCount) {
      var shown = state.items.length;
      var total = state.total > shown ? state.total : shown;
      els.headCount.textContent = state.loading
        ? "Loading\u2026"
        : total === 0
          ? "No records"
          : total === shown
            ? shown + (shown === 1 ? " record" : " records")
            : shown + " of " + total + " records";
    }
    if (els.headTime) {
      els.headTime.textContent = state.updatedAt || "";
    }
  }

  function detailRow(label, value, options) {
    var opts = options || {};
    if (!value) {
      return "";
    }

    var esc = core.escapeHTML;
    var cell = opts.href
      ? '<a class="sheet-link" href="' +
        esc(opts.href) +
        '" target="_blank" rel="noopener noreferrer">' +
        esc(value) +
        "</a>"
      : '<span class="sheet-value">' + esc(value) + "</span>";

    return '<div class="sheet-row"><span class="sheet-label">' + esc(label) + "</span>" + cell + "</div>";
  }

  function renderSheet(item) {
    var esc = core.escapeHTML;
    var start = formatDate(item.startDate);
    var end = formatDate(item.endDate);

    sheet.title.textContent = item.creditName || "Alarm record";

    var links = "";
    if (item.pageUrl1) {
      links += detailRow("公告链接", item.noticeId || "打开", { href: item.pageUrl1 });
    }
    if (item.pageUrl2) {
      links += detailRow("原始公告", item.originNoticeId || "打开", { href: item.pageUrl2 });
    }

    sheet.body.innerHTML =
      (item.title ? '<p class="sheet-lead">' + esc(item.title) + "</p>" : "") +
      '<div class="sheet-rows">' +
      detailRow("主体", item.creditName) +
      detailRow("信用代码", item.creditCode) +
      detailRow("开始时间", start) +
      detailRow("结束时间", end) +
      detailRow("处罚部门", item.handleDepartment) +
      detailRow("处罚单位", item.handleUnit) +
      detailRow("具体情形", item.detailReason) +
      detailRow("处罚结果", item.handleResult) +
      detailRow("记录编号", item.businessId) +
      links +
      "</div>";

    sheet.actions.innerHTML = "";
    if (item.link) {
      var open = document.createElement("button");
      open.type = "button";
      open.className = "btn";
      open.textContent = "Open in chat";
      open.addEventListener("click", function () {
        // Deep links stay inside Telegram: openLink would hand them to the
        // system browser and bounce back.
        core.openTelegramLink(item.link);
        closeSheet();
      });
      sheet.actions.appendChild(open);
    }

    var close = document.createElement("button");
    close.type = "button";
    close.className = "btn-ghost";
    close.textContent = "Close";
    close.addEventListener("click", closeSheet);
    sheet.actions.appendChild(close);

    // Links are ordinary web targets, so they keep the browser.
    var anchors = sheet.body.querySelectorAll("a.sheet-link");
    Array.prototype.forEach.call(anchors, function (anchor) {
      anchor.addEventListener("click", function (event) {
        event.preventDefault();
        core.openLink(anchor.getAttribute("href"));
      });
    });
  }

  function openSheet(item) {
    sheet.lastFocus = document.activeElement;
    renderSheet(item);
    sheet.el.hidden = false;
    if (sheet.body.scrollTop) {
      sheet.body.scrollTop = 0;
    }
    document.body.classList.add("is-sheet-open");
    var first = sheet.actions.querySelector("button");
    if (first) {
      first.focus({ preventScroll: true });
    }
  }

  function closeSheet() {
    if (!sheet.el || sheet.el.hidden) {
      return;
    }

    sheet.el.hidden = true;
    document.body.classList.remove("is-sheet-open");
    if (sheet.lastFocus && typeof sheet.lastFocus.focus === "function") {
      sheet.lastFocus.focus({ preventScroll: true });
    }
    sheet.lastFocus = null;
  }

  /* Data ------------------------------------------------------------------ */

  function loadRecords(reset) {
    if (!reset && state.nextOffset === null) {
      return Promise.resolve();
    }
    // Bumping the token before the request means any response still in flight
    // belongs to a superseded query and is dropped when it lands.
    var token = (state.seq += 1);
    state.status = "loading";
    if (reset) {
      state.items = [];
      state.total = 0;
    }
    render();

    var offset = reset ? 0 : state.nextOffset;

    return Promise.resolve()
      .then(function () {
        return api().alarmRecords({ q: state.q, offset: offset, limit: PAGE_SIZE });
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
        var now = new Date();
        state.updatedAt =
          "updated " + (now.getHours() < 10 ? "0" : "") + now.getHours() + ":" +
          (now.getMinutes() < 10 ? "0" : "") + now.getMinutes();
        render();
      })
      .catch(function () {
        if (token !== state.seq) {
          return;
        }
        state.status = "error";
        render();
      });
  }

  /* Events ---------------------------------------------------------------- */

  function onListClick(event) {
    var target = event.target;
    var button =
      target && typeof target.closest === "function" ? target.closest("button[data-id]") : null;
    if (!button) {
      return;
    }

    var id = button.getAttribute("data-id");
    for (var i = 0; i < state.items.length; i++) {
      if (state.items[i].businessId === id) {
        openSheet(state.items[i]);
        return;
      }
    }
  }

  function onKeydown(event) {
    if (event.key === "Escape") {
      closeSheet();
    }
  }

  /* View ------------------------------------------------------------------ */

  function slot(root, name) {
    return root.querySelector('[data-slot="' + name + '"]');
  }

  function build(section) {
    section.innerHTML = TEMPLATE;

    var search = section.querySelector('[data-role="search"]');

    sheet.el = section.querySelector('[data-role="sheet"]');
    sheet.title = slot(section, "sheet-title");
    sheet.body = slot(section, "sheet-body");
    sheet.actions = slot(section, "sheet-actions");

    return {
      search: search,
      headCount: slot(section, "head-count"),
      headTime: slot(section, "head-time"),
      loading: slot(section, "loading"),
      error: slot(section, "error"),
      empty: slot(section, "empty"),
      list: slot(section, "list"),
      loadMore: section.querySelector('[data-role="load-more"]'),
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

    els.search.addEventListener("input", runSearch);
    els.list.addEventListener("click", onListClick);
    els.loadMore.addEventListener("click", function () {
      if (state.status !== "loading") {
        loadRecords(false);
      }
    });

    els.sheetClose.addEventListener("click", closeSheet);
    sheet.el.addEventListener("click", function (event) {
      // The backdrop closes; a click inside the panel does not.
      if (event.target === sheet.el) {
        closeSheet();
      }
    });
    document.addEventListener("keydown", onKeydown);
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

    if (!state.loaded || core.state.isStale(VIEW)) {
      loadRecords(true);
    }
  }

  core.registerView(VIEW, { mount: mount });
})(window);
