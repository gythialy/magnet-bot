/*
 * keywords.js — the Keywords view (read + write).
 *
 * Renders the user's project keywords inside #view-keywords: search, one row per
 * keyword, and the three write flows (batch add, rename, delete) in a single
 * reusable dialog. js/core.js owns the shell (routing, Telegram bootstrap,
 * transport); this file only knows what a keyword is.
 *
 * Read states: loading -> ready (list or empty) | error (retry). The search field
 * filters the already loaded list in place; it never issues a request.
 *
 * Write rules, all of them deliberate:
 *   - the confirm button is disabled while the field is empty and while a
 *     request is in flight, so a double tap cannot submit twice;
 *   - rename updates the row from the server's echoed value instead of
 *     refetching the whole list, delete removes the row in place;
 *   - any successful write invalidates "keywords" and "today" (the statistics
 *     bar counts keywords) so the next visit reloads instead of trusting a
 *     stale render;
 *   - 429 and 404 are translated into copy the user can act on.
 */
(function (global) {
  "use strict";

  var core = global.MagnetApp;
  if (!core) {
    // index.html loads js/core.js first; without it there is no transport and no
    // view registry, so this script must not pretend to render anything.
    return;
  }

  var VIEW = "keywords";
  var SECTION_ID = "view-keywords";
  var ADD_HINT = "Comma-separated, e.g. 服务器, 防火墙";
  var DELETE_TEXT = "The keyword is removed. Notices already delivered stay in your history.";
  var MAX_KEYWORD_LENGTH = 128;

  var els = null;
  var items = [];
  var query = "";
  var status = "idle";
  var loaded = false;

  var TEMPLATE =
    '<div class="kw-toolbar">' +
    '<input class="search-input" type="search" data-role="search" placeholder="Search keywords" ' +
    'aria-label="Search keywords" autocomplete="off" />' +
    '<button type="button" class="btn kw-add" data-role="add">+ Add</button>' +
    "</div>" +
    '<div class="skeleton" data-slot="loading">Loading\u2026</div>' +
    '<div class="error" data-slot="error" role="alert" hidden></div>' +
    '<div class="empty" data-slot="empty" hidden></div>' +
    '<ul class="kw-list" data-slot="list" hidden></ul>' +
    '<div class="modal-backdrop kw-dialog" data-role="dialog" hidden>' +
    '<div class="modal" role="dialog" aria-modal="true" aria-labelledby="kw-dialog-title">' +
    '<div class="modal-head">' +
    '<h2 class="modal-title" id="kw-dialog-title" data-slot="dialog-title"></h2>' +
    '<button class="modal-close" type="button" data-role="dialog-close" aria-label="Close dialog">' + core.icon("close") + '</button>' +
    "</div>" +
    '<div class="dialog-body" data-slot="dialog-body"></div>' +
    '<div class="dialog-actions">' +
    '<button class="btn-ghost" type="button" data-role="dialog-cancel">Cancel</button>' +
    '<button class="btn" type="button" data-role="dialog-confirm" data-slot="dialog-confirm">Save</button>' +
    "</div>" +
    "</div>" +
    "</div>";

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

  function api() {
    var apiNamespace = global.MagnetAPI;
    if (!apiNamespace) {
      throw new Error("MagnetAPI is not loaded");
    }
    return apiNamespace;
  }

  /* Helpers -------------------------------------------------------------- */

  // Thousands separators without pulling in Intl: the counter is an integer and
  // the grouping is the only thing a list needs.
  function formatCount(value) {
    var number = Number(value);
    if (!isFinite(number)) {
      return "0";
    }

    var digits = String(Math.trunc(Math.abs(number)));
    var grouped = "";

    for (var i = 0; i < digits.length; i++) {
      if (i > 0 && (digits.length - i) % 3 === 0) {
        grouped += ",";
      }
      grouped += digits[i];
    }

    return (number < 0 ? "-" : "") + grouped;
  }

  function normalize(payload) {
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
        counter: typeof item.counter === "number" && isFinite(item.counter) ? item.counter : 0,
      });
    }

    return result;
  }

  function filteredItems() {
    var needle = query.trim().toLowerCase();
    if (!needle) {
      return items;
    }

    return items.filter(function (item) {
      return item.keyword.toLowerCase().indexOf(needle) !== -1;
    });
  }

  function findItem(id) {
    var wanted = String(id);
    for (var i = 0; i < items.length; i++) {
      if (String(items[i].id) === wanted) {
        return items[i];
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

  function rowHTML(item) {
    var esc = core.escapeHTML;
    var counter = item.counter === 1 ? "1 notice" : formatCount(item.counter) + " notices";

    return (
      '<li class="kw-row">' +
      '<span class="kw-id">#' +
      esc(String(item.id)) +
      "</span>" +
      '<span class="kw-text">' +
      '<span class="kw-name" title="' +
      esc(item.keyword) +
      '">' +
      esc(item.keyword) +
      "</span>" +
      '<span class="kw-count">' +
      esc(counter) +
      "</span>" +
      "</span>" +
      '<span class="kw-actions">' +
      iconButton("rename", item.id, "Rename keyword", "edit") +
      iconButton("delete", item.id, "Delete keyword", "trash") +
      "</span>" +
      "</li>"
    );
  }

  function renderRetry(message) {
    els.error.textContent = "";

    var text = document.createElement("span");
    text.className = "error-text";
    text.textContent = message;

    var actions = document.createElement("div");
    actions.className = "error-actions";

    var retry = document.createElement("button");
    retry.type = "button";
    retry.className = "btn-ghost retry-btn";
    retry.textContent = "Retry";
    retry.addEventListener("click", function () {
      load();
    });

    actions.appendChild(retry);
    els.error.appendChild(text);
    els.error.appendChild(actions);
  }

  function render() {
    if (!els) {
      return;
    }

    var visible = filteredItems();
    var showEmpty = status === "ready" && visible.length === 0;
    var showList = status === "ready" && visible.length > 0;

    els.loading.hidden = status !== "loading";
    els.error.hidden = status !== "error";
    els.empty.hidden = !showEmpty;
    els.list.hidden = !showList;

    if (status === "error") {
      renderRetry("Could not load keywords.");
    }

    if (showEmpty) {
      els.empty.textContent =
        items.length === 0
          ? "No keywords yet. Add keywords from the bot or with + Add."
          : 'No keywords match "' + query.trim() + '".';
    }

    if (showList) {
      els.list.innerHTML = visible.map(rowHTML).join("");
    } else {
      // Clear stale rows when the list is hidden, so the document never keeps
      // rows that no longer exist in the model (the empty state replaces them).
      els.list.innerHTML = "";
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
    // Enter for a newline because a keyword may contain a comma list per line.
    if (key === "Enter" && dialog.field && dialog.field.tagName === "INPUT" && !dialog.pending) {
      event.preventDefault();
      confirmDialog();
    }
  }

  /* Write actions -------------------------------------------------------- */

  function invalidateWrites() {
    // The statistics bar counts keywords, so Today has to reload as well.
    core.state.invalidate("keywords");
    core.state.invalidate("today");
  }

  function handleWriteError(err) {
    var statusCode = err && err.status;

    if (statusCode === 429) {
      core.toast("Too many requests, try again in a minute", "error");
      return;
    }
    if (statusCode === 404) {
      closeDialog();
      core.toast("That keyword no longer exists", "error");
      load();
      return;
    }

    core.toast((err && err.message) || "Something went wrong. Please try again.", "error");
  }

  function openAddDialog() {
    var textarea = document.createElement("textarea");
    textarea.className = "dialog-textarea";
    textarea.rows = 3;
    textarea.placeholder = ADD_HINT;
    textarea.setAttribute("aria-label", "Keywords, comma-separated");
    textarea.value = "";
    textarea.addEventListener("input", refreshConfirm);

    openDialog({
      title: "Add keywords",
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
          .createKeywords(raw)
          .then(function (payload) {
            var added = payload && Array.isArray(payload.added) ? payload.added : [];
            var skipped = payload && Array.isArray(payload.skipped) ? payload.skipped : [];
            var message = "Added " + added.length;
            if (skipped.length > 0) {
              message += ", skipped " + skipped.length;
            }

            core.toast(message, "success");
            closeDialog();
            invalidateWrites();
            return load();
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
    input.setAttribute("aria-label", "Keyword");
    input.addEventListener("input", refreshConfirm);

    openDialog({
      title: "Rename keyword",
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
          .renameKeyword(item.id, next)
          .then(function (payload) {
            item.keyword = payload && typeof payload.keyword === "string" ? payload.keyword : next;
            render();
            core.toast("Keyword updated", "success");
            closeDialog();
            invalidateWrites();
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
      title: "Delete keyword",
      content: message,
      confirmLabel: "Delete",
      pendingLabel: "Deleting\u2026",
      danger: true,
      // Cancel is the safe default for a destructive action.
      focus: dialog.cancel,
      onConfirm: function () {
        return api()
          .deleteKeyword(item.id)
          .then(function () {
            var index = items.indexOf(item);
            if (index !== -1) {
              items.splice(index, 1);
            }
            render();
            core.toast("Keyword deleted", "success");
            closeDialog();
            invalidateWrites();
          })
          .catch(handleWriteError);
      },
    });
  }

  /* Data ----------------------------------------------------------------- */

  function load() {
    if (status === "loading") {
      return Promise.resolve();
    }

    status = "loading";
    render();

    return Promise.resolve()
      .then(function () {
        return api().keywords();
      })
      .then(function (payload) {
        items = normalize(payload);
        loaded = true;
        status = "ready";
        render();
      })
      .catch(function () {
        status = "error";
        render();
      });
  }

  /* View ---------------------------------------------------------------- */

  function build(section) {
    section.innerHTML = TEMPLATE;

    dialog.el = section.querySelector('[data-role="dialog"]');
    dialog.title = section.querySelector('[data-slot="dialog-title"]');
    dialog.body = section.querySelector('[data-slot="dialog-body"]');
    dialog.confirm = section.querySelector('[data-role="dialog-confirm"]');
    dialog.cancel = section.querySelector('[data-role="dialog-cancel"]');
    dialog.close = section.querySelector('[data-role="dialog-close"]');

    return {
      search: section.querySelector('[data-role="search"]'),
      add: section.querySelector('[data-role="add"]'),
      loading: section.querySelector('[data-slot="loading"]'),
      error: section.querySelector('[data-slot="error"]'),
      empty: section.querySelector('[data-slot="empty"]'),
      list: section.querySelector('[data-slot="list"]'),
    };
  }

  function onListClick(event) {
    var target = event.target;
    var button =
      target && typeof target.closest === "function" ? target.closest("button[data-action]") : null;
    if (!button) {
      return;
    }

    var item = findItem(button.getAttribute("data-id"));
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

  function bind() {
    els.search.addEventListener("input", function () {
      query = els.search.value;
      render();
    });

    els.add.addEventListener("click", openAddDialog);
    els.list.addEventListener("click", onListClick);

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

    // A write invalidates this view; reading the flag here is what turns that
    // into exactly one reload on the next visit.
    if (!loaded || core.state.isStale(VIEW)) {
      return load();
    }
  }

  core.registerView(VIEW, { mount: mount });
})(window);
