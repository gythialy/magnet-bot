/*
 * alarms.js — the Alarms view (read + write).
 *
 * Owns the user's alarm keywords: id, name, created on, plus the batch-add /
 * rename / delete flows in a reusable dialog. Alarm records used to share this
 * section; they are a browsing surface with their own paging and detail sheet
 * and now live in js/records.js under the Records tab.
 *
 * A successful write invalidates "alarms" and "today" (the statistics bar
 * counts alarm keywords).
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
  var ADD_HINT = "Comma-separated, e.g. 服务器, 防火墙";
  var DELETE_TEXT =
    "The alarm keyword is removed. Alarm records already delivered stay in your history.";
  var MAX_KEYWORD_LENGTH = 128;

  var els = null;

  var keywords = { items: [], status: "idle", loaded: false };
  // The list is small and already loaded, so the search filters it in place and
  // never issues a request — the same contract as the Keywords page.
  var query = "";
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
    '<div class="alarms-toolbar">' +
    '<input class="search-input" type="search" data-role="search" ' +
    'placeholder="Search alarm keywords" aria-label="Search alarm keywords" autocomplete="off" />' +
    '<button type="button" class="btn alarms-add" data-role="add-alarm">+ Add</button>' +
    "</div>" +
    '<div class="skeleton" data-slot="loading">Loading\u2026</div>' +
    '<div class="error" data-slot="error" role="alert" hidden></div>' +
    '<div class="empty" data-slot="empty" hidden></div>' +
    '<ul class="alarm-kw-list" data-slot="list" hidden></ul>' +
    "</section>" +
    '<div class="modal-backdrop" data-role="dialog" hidden>' +
    '<div class="modal" role="dialog" aria-modal="true" aria-labelledby="alarms-dialog-title">' +
    '<div class="modal-head">' +
    '<h2 class="modal-title" id="alarms-dialog-title" data-slot="dialog-title"></h2>' +
    '<button class="modal-close" type="button" data-role="dialog-close" aria-label="Close dialog">' + core.icon("close") + '</button>' +
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

  function filteredKeywords() {
    var needle = query.trim().toLowerCase();
    if (!needle) {
      return keywords.items;
    }

    return keywords.items.filter(function (item) {
      return item.keyword.toLowerCase().indexOf(needle) !== -1;
    });
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
    // glyph is the icon name in the shared family, never a character.
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
      core.icon(glyph) +
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
      iconButton("rename", item.id, "Rename alarm keyword", "edit") +
      iconButton("delete", item.id, "Delete alarm keyword", "trash") +
      "</span>" +
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
    var visible = filteredKeywords();
    var showList = state.status === "ready" && visible.length > 0;
    var showEmpty = state.status === "ready" && visible.length === 0;

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
        state.items.length === 0
          ? "No alarm keywords yet. Add alarm keywords from the bot or with + Add."
          : 'No alarm keywords match "' + query.trim() + '".';
    }

    if (showList) {
      els.kwList.innerHTML = visible.map(alarmKeywordRow).join("");
    } else {
      // Clear stale rows when the list is hidden; the empty state replaces them.
      els.kwList.innerHTML = "";
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

  // The Records tab reads the same account, so a keyword edit also marks it
  // stale; it reloads the next time it is opened instead of keeping a history
  // that predates the change.
  function refreshAfterWrite() {
    invalidateWrites();
    core.state.invalidate("records");
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

  /* Events --------------------------------------------------------------- */

  function onSearchInput() {
    query = els.search.value;
    renderKeywordBlock();
  }

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

  /* View ---------------------------------------------------------------- */

  function slot(root, name) {
    return root.querySelector('[data-slot="' + name + '"]');
  }

  function build(section) {
    section.innerHTML = TEMPLATE;

    var keywordBlock = section.querySelector('[data-block="alarm-keywords"]');

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
      search: section.querySelector('[data-role="search"]'),
      addAlarm: section.querySelector('[data-role="add-alarm"]'),
    };
  }

  function bind() {
    els.search.addEventListener("input", onSearchInput);
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

    // A write invalidates the view, which is what makes the next visit reload
    // instead of showing the edited list.
    if (!keywords.loaded || core.state.isStale(VIEW)) {
      loadKeywords();
    }
  }

  core.registerView(VIEW, { mount: mount });
})(window);
