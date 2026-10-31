/*
 * alarms.js — the Alarms view (read + write).
 *
 * Two independent blocks share one section:
 *   1. "Alarm keywords" — the user's alarm keywords, id/name/created on, plus
 *      the batch-add / rename / delete flows in a reusable dialog.
 *   2. "Alarm records"  — paged alert history with a debounced server-side
 *      search and a deep link back into the bot for each row.
 *
 * Each block keeps its own loading/empty/error state so a slow or failing
 * records request never blanks the keyword list above it. Records page with the
 * API's nextOffset cursor; "Load more" disappears once the cursor is null.
 *
 * Write rules mirror the Keywords page, with two additions: a successful write
 * invalidates "alarms" and "today" (the statistics bar counts alarm keywords),
 * and it reloads the records block in place so the page never shows a stale
 * history next to freshly edited keywords.
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

  var ADD_HINT = "Comma-separated, e.g. 服务器, 防火墙";
  var DELETE_TEXT =
    "The alarm keyword is removed. Alarm records already delivered stay in your history.";
  var MAX_KEYWORD_LENGTH = 128;

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

  var dialog = {
    el: null,
    title: null,
    body: null,
    confirm: null,
    cancel: null,
    close: null,
    field: null,
    onConfirm: null,
    confirmLabel: "Save",
    pendingLabel: "Saving\u2026",
    pending: false,
    danger: false,
    lastFocus: null,
  };

  var TEMPLATE =
    '<section class="alarms-block" data-block="alarm-keywords">' +
    '<div class="block-head">' +
    '<h2 class="block-title">Alarm keywords</h2>' +
    '<button type="button" class="btn block-add" data-role="add-alarm">+ Add alarm keyword</button>' +
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
    "</section>" +
    '<div class="modal-backdrop alarms-dialog" data-role="dialog" hidden>' +
    '<div class="modal" role="dialog" aria-modal="true" aria-labelledby="alarms-dialog-title">' +
    '<div class="modal-head">' +
    '<h2 class="modal-title" id="alarms-dialog-title" data-slot="dialog-title"></h2>' +
    '<button class="modal-close" type="button" data-role="dialog-close" aria-label="Close dialog">\u2715</button>' +
    "</div>" +
    '<div class="dialog-body" data-slot="dialog-body"></div>' +
    '<div class="dialog-actions">' +
    '<button class="btn-ghost" type="button" data-role="dialog-cancel">Cancel</button>' +
    '<button class="btn" type="button" data-role="dialog-confirm" data-slot="dialog-confirm">Save</button>' +
    "</div>" +
    "</div>" +
    "</div>";

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

    return parsed.getFullYear() + "-" + pad2(parsed.getMonth() + 1) + "-" + pad2(parsed.getDate());
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

  function findKeyword(id) {
    var wanted = String(id);
    for (var i = 0; i < keywords.items.length; i++) {
      if (String(keywords.items[i].id) === wanted) {
        return keywords.items[i];
      }
    }
    return null;
  }

  /* Rendering ------------------------------------------------------------ */

  function iconButton(action, id, label, glyph) {
    return (
      '<button type="button" class="icon-btn" data-action="' +
      action +
      '" data-id="' +
      core.escapeHTML(String(id)) +
      '" title="' +
      core.escapeHTML(label) +
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
      iconButton("rename", item.id, "Rename alarm keyword", "\u270f\ufe0f") +
      iconButton("delete", item.id, "Delete alarm keyword", "\ud83d\uddd1") +
      "</span>" +
      "</li>"
    );
  }

  function recordLinkButton(link) {
    var label = "Open alarm record in Telegram";

    if (!link) {
      return (
        '<button type="button" class="icon-btn" disabled title="No link available" ' +
        'aria-label="' +
        core.escapeHTML(label) +
        '">\u2197</button>'
      );
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
        "No alarm keywords yet. Add alarm keywords from the bot or with + Add alarm keyword.";
    }

    if (showList) {
      els.kwList.innerHTML = state.items.map(alarmKeywordRow).join("");
    } else {
      // Clear stale rows when the list is hidden; the empty state replaces them.
      els.kwList.innerHTML = "";
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
    } else {
      els.recList.innerHTML = "";
    }
  }

  /* Dialog --------------------------------------------------------------- */

  function refreshConfirm() {
    if (!dialog.el) {
      return;
    }

    var empty = dialog.field ? dialog.field.value.trim() === "" : false;
    dialog.confirm.disabled = dialog.pending || empty;
    dialog.cancel.disabled = dialog.pending;
    dialog.close.disabled = dialog.pending;
  }

  function setDialogPending(pending) {
    dialog.pending = pending;
    dialog.confirm.textContent = pending ? dialog.pendingLabel : dialog.confirmLabel;
    refreshConfirm();
  }

  function openDialog(config) {
    dialog.title.textContent = config.title;
    dialog.body.textContent = "";
    dialog.body.appendChild(config.content);

    dialog.field = config.field || null;
    dialog.onConfirm = config.onConfirm;
    dialog.confirmLabel = config.confirmLabel || "Save";
    dialog.pendingLabel = config.pendingLabel || "Saving\u2026";
    dialog.danger = Boolean(config.danger);
    dialog.pending = false;

    dialog.confirm.textContent = dialog.confirmLabel;
    dialog.confirm.className = dialog.danger ? "btn is-danger" : "btn";
    dialog.cancel.disabled = false;
    dialog.close.disabled = false;

    dialog.lastFocus = document.activeElement;
    dialog.el.hidden = false;
    refreshConfirm();

    var focusTarget = config.focus || dialog.confirm;
    if (focusTarget && typeof focusTarget.focus === "function") {
      focusTarget.focus();
    }
  }

  function closeDialog() {
    if (!dialog.el || dialog.el.hidden) {
      return;
    }

    dialog.el.hidden = true;
    dialog.body.textContent = "";
    dialog.field = null;
    dialog.onConfirm = null;
    dialog.pending = false;

    if (dialog.lastFocus && typeof dialog.lastFocus.focus === "function") {
      dialog.lastFocus.focus();
    }
    dialog.lastFocus = null;
  }

  function confirmDialog() {
    if (dialog.pending || !dialog.onConfirm) {
      return;
    }

    var result;
    try {
      result = dialog.onConfirm();
    } catch (err) {
      handleWriteError(err);
      return;
    }

    if (!result || typeof result.then !== "function") {
      return;
    }

    setDialogPending(true);
    result.then(
      function () {
        setDialogPending(false);
      },
      function () {
        setDialogPending(false);
      }
    );
  }

  function onDialogKeydown(event) {
    if (!dialog.el || dialog.el.hidden) {
      return;
    }

    var key = event.key || "";
    if (key === "Escape" || key === "Esc") {
      event.preventDefault();
      closeDialog();
      return;
    }

    // Enter submits from the single-line rename field; the batch textarea keeps
    // Enter for a newline.
    if (key === "Enter" && dialog.field && dialog.field.tagName === "INPUT" && !dialog.pending) {
      event.preventDefault();
      confirmDialog();
    }
  }

  /* Write actions -------------------------------------------------------- */

  function invalidateWrites() {
    // The statistics bar counts alarm keywords, so Today has to reload as well.
    core.state.invalidate("alarms");
    core.state.invalidate("today");
  }

  // Every successful write reloads the records block: its rows come from the
  // same account, and the page must not show a history that predates the edit
  // the user just confirmed.
  function refreshAfterWrite() {
    invalidateWrites();
    loadRecords(true);
  }

  function handleWriteError(err) {
    var statusCode = err && err.status;

    if (statusCode === 429) {
      core.toast("Too many requests, try again in a minute", "error");
      return;
    }
    if (statusCode === 404) {
      closeDialog();
      core.toast("That alarm keyword no longer exists", "error");
      loadKeywords();
      loadRecords(true);
      return;
    }

    core.toast((err && err.message) || "Something went wrong. Please try again.", "error");
  }

  function openAddDialog() {
    var textarea = document.createElement("textarea");
    textarea.className = "dialog-textarea";
    textarea.rows = 3;
    textarea.placeholder = ADD_HINT;
    textarea.setAttribute("aria-label", "Alarm keywords, comma-separated");
    textarea.value = "";
    textarea.addEventListener("input", refreshConfirm);

    openDialog({
      title: "Add alarm keywords",
      content: textarea,
      field: textarea,
      confirmLabel: "Add",
      pendingLabel: "Adding\u2026",
      focus: textarea,
      onConfirm: function () {
        var raw = textarea.value.trim();
        if (!raw) {
          return;
        }

        return api()
          .createAlarms(raw)
          .then(function (payload) {
            var added = payload && Array.isArray(payload.added) ? payload.added : [];
            var skipped = payload && Array.isArray(payload.skipped) ? payload.skipped : [];
            var message = "Added " + added.length;
            if (skipped.length > 0) {
              message += ", skipped " + skipped.length;
            }

            core.toast(message, "success");
            closeDialog();
            return loadKeywords().then(refreshAfterWrite);
          })
          .catch(handleWriteError);
      },
    });
  }

  function openRenameDialog(item) {
    var input = document.createElement("input");
    input.type = "text";
    input.className = "dialog-input";
    input.value = item.keyword;
    input.maxLength = MAX_KEYWORD_LENGTH;
    input.setAttribute("aria-label", "Alarm keyword");
    input.addEventListener("input", refreshConfirm);

    openDialog({
      title: "Rename alarm keyword",
      content: input,
      field: input,
      confirmLabel: "Save",
      pendingLabel: "Saving\u2026",
      focus: input,
      onConfirm: function () {
        var next = input.value.trim();
        if (!next) {
          return;
        }

        return api()
          .renameAlarm(item.id, next)
          .then(function (payload) {
            item.keyword = payload && typeof payload.keyword === "string" ? payload.keyword : next;
            renderKeywordBlock();
            core.toast("Alarm keyword updated", "success");
            closeDialog();
            refreshAfterWrite();
          })
          .catch(handleWriteError);
      },
    });
  }

  function openDeleteDialog(item) {
    var message = document.createElement("p");
    message.className = "dialog-message";
    message.textContent = DELETE_TEXT;

    openDialog({
      title: "Delete alarm keyword",
      content: message,
      confirmLabel: "Delete",
      pendingLabel: "Deleting\u2026",
      danger: true,
      // Cancel is the safe default for a destructive action.
      focus: dialog.cancel,
      onConfirm: function () {
        return api()
          .deleteAlarm(item.id)
          .then(function () {
            var index = keywords.items.indexOf(item);
            if (index !== -1) {
              keywords.items.splice(index, 1);
            }
            renderKeywordBlock();
            core.toast("Alarm keyword deleted", "success");
            closeDialog();
            refreshAfterWrite();
          })
          .catch(handleWriteError);
      },
    });
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

  function onKeywordListClick(event) {
    var target = event.target;
    var button =
      target && typeof target.closest === "function" ? target.closest("button[data-action]") : null;
    if (!button) {
      return;
    }

    var item = findKeyword(button.getAttribute("data-id"));
    if (!item) {
      return;
    }

    var action = button.getAttribute("data-action");
    if (action === "rename") {
      openRenameDialog(item);
    } else if (action === "delete") {
      openDeleteDialog(item);
    }
  }

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

    dialog.el = section.querySelector('[data-role="dialog"]');
    dialog.title = section.querySelector('[data-slot="dialog-title"]');
    dialog.body = section.querySelector('[data-slot="dialog-body"]');
    dialog.confirm = section.querySelector('[data-role="dialog-confirm"]');
    dialog.cancel = section.querySelector('[data-role="dialog-cancel"]');
    dialog.close = section.querySelector('[data-role="dialog-close"]');

    return {
      kwLoading: slot(keywordBlock, "loading"),
      kwError: slot(keywordBlock, "error"),
      kwEmpty: slot(keywordBlock, "empty"),
      kwList: slot(keywordBlock, "list"),
      recLoading: slot(recordBlock, "loading"),
      recError: slot(recordBlock, "error"),
      recEmpty: slot(recordBlock, "empty"),
      recList: slot(recordBlock, "list"),
      addAlarm: section.querySelector('[data-role="add-alarm"]'),
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

    els.addAlarm.addEventListener("click", openAddDialog);
    els.kwList.addEventListener("click", onKeywordListClick);

    dialog.close.addEventListener("click", closeDialog);
    dialog.cancel.addEventListener("click", closeDialog);
    dialog.confirm.addEventListener("click", confirmDialog);
    dialog.el.addEventListener("click", function (event) {
      // The backdrop closes; a click inside the panel does not.
      if (event.target === dialog.el) {
        closeDialog();
      }
    });

    document.addEventListener("keydown", onDialogKeydown);
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
    // other, and each owns its skeleton. A write invalidates the view, which is
    // what makes the next visit reload instead of showing the edited list.
    if (!keywords.loaded || core.state.isStale(VIEW)) {
      loadKeywords();
    }
    if (!records.loaded || core.state.isStale(VIEW)) {
      return loadRecords(true);
    }
  }

  core.registerView(VIEW, { mount: mount });
})(window);
