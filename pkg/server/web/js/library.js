/*
 * library.js — the merged watchlist (keywords + alarm words).
 *
 * One page, two word types, because they answer the same question: what does
 * the bot watch for me. The segment switches the type; the list shows active
 * words only, and archived words stay one switch away in their own group —
 * they never mix into the active list.
 *
 * Rules this file owns:
 *   - archiving is reversible: the row moves groups immediately and the toast
 *     offers Undo for five seconds; archived words stop matching on the server
 *   - deleting is destructive and names the word in a confirmation sheet
 *   - a failed refresh keeps the last rows and says how old they are; it never
 *     turns into a fake empty list
 *   - the administrator read-only mode renders no write controls at all, keeps
 *     search, and exits back to the caller's own library
 *   - switching language only rewrites text nodes; the list, focus, the open
 *     sheet and the scroll position survive
 */
(function (global) {
  "use strict";

  var core = global.MagnetApp;
  if (!core) {
    return;
  }

  var i18n = global.MagnetI18n;
  var VIEW = "library";
  var SECTION_ID = "view-library";
  var STORAGE_ARCHIVED = "magnet.showArchived";
  var MAX_KEYWORD_LENGTH = 128;

  var TYPES = {
    keywords: {
      api: "keywords",
      adminType: "project",
      unit: "unit.keywords",
      searchPh: "lib.searchPh",
      emptyTitle: "lib.emptyTitle",
      emptyNote: "lib.emptyNote",
      emptyMatch: "lib.emptyMatch",
      loadFailed: "lib.loadFailed",
      renameTitle: "lib.renameTitle",
      deleteTitle: "lib.deleteTitle",
      addTitle: "lib.addTitle",
      update: "updateKeyword",
      create: "createKeywords",
    },
    alarms: {
      api: "alarms",
      adminType: "alarm",
      unit: "unit.alarms",
      searchPh: "lib.searchAlarms",
      emptyTitle: "lib.emptyAlarmsTitle",
      emptyNote: "lib.emptyAlarmsNote",
      emptyMatch: "lib.emptyMatchAlarms",
      loadFailed: "lib.loadFailedAlarms",
      renameTitle: "lib.renameAlarmTitle",
      deleteTitle: "lib.deleteAlarmTitle",
      addTitle: "lib.addAlarmTitle",
      update: "updateAlarm",
      create: "createAlarms",
    },
  };

  var state = {
    type: "keywords",
    q: "",
    showArchived: readShowArchived(),
    items: [],
    archivedCount: 0,
    status: "idle",
    loaded: false,
    loadedAt: null,
    failedAt: null,
    viewing: null,
    me: null,
    stats: null,
    scrollTop: 0,
  };

  var els = null;
  var dialog = null;

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

  function readShowArchived() {
    try {
      return global.localStorage.getItem(STORAGE_ARCHIVED) === "1";
    } catch (err) {
      return false;
    }
  }

  function storeShowArchived(value) {
    try {
      if (value) {
        global.localStorage.setItem(STORAGE_ARCHIVED, "1");
      } else {
        global.localStorage.removeItem(STORAGE_ARCHIVED);
      }
    } catch (err) {
      // The toggle still works for this session when storage is unavailable.
    }
  }

  function meta() {
    return TYPES[state.type];
  }

  function roleLabel(user) {
    if (user && user.enabled === false) {
      return t("set.disabled");
    }
    return t(user && user.role === "admin" ? "role.admin" : "role.user");
  }

  /* Template ------------------------------------------------------------- */

  var TEMPLATE =
    '<div class="screen" data-role="screen">' +
    core.topbarHTML() +
    '<div data-slot="robar" hidden></div>' +
    '<div class="view-head">' +
    '<div class="view-head-row">' +
    "<div>" +
    '<h2 class="view-title" data-i18n="lib.title">' +
    esc(t("lib.title")) +
    "</h2>" +
    '<p class="view-sub" data-slot="sub"></p>' +
    "</div>" +
    '<button type="button" class="head-action" data-role="members" hidden>' +
    core.icon("users", "icon-sm") +
    '<span data-i18n="lib.members">' +
    esc(t("lib.members")) +
    "</span>" +
    "</button>" +
    "</div>" +
    "</div>" +
    '<div class="controls-band">' +
    '<div class="segmented" role="group" data-slot="segmented" data-i18n-aria="aria.wordType" aria-label="' +
    esc(t("aria.wordType")) +
    '">' +
    '<button type="button" data-type="keywords" data-i18n="lib.tabKeywords">' +
    esc(t("lib.tabKeywords")) +
    "</button>" +
    '<button type="button" data-type="alarms" data-i18n="lib.tabAlarms">' +
    esc(t("lib.tabAlarms")) +
    "</button>" +
    "</div>" +
    '<div class="toolbar is-flush">' +
    '<label class="search">' +
    core.icon("search", "icon-sm") +
    '<input type="search" data-role="search" autocomplete="off" data-i18n-ph="lib.searchPh" />' +
    "</label>" +
    '<button type="button" class="btn" data-role="add">' +
    core.icon("plus", "icon-sm") +
    '<span data-i18n="lib.add">' +
    esc(t("lib.add")) +
    "</span>" +
    "</button>" +
    "</div>" +
    "</div>" +
    '<div class="list-head" data-slot="listhead" hidden>' +
    '<span data-slot="count"></span>' +
    '<label class="toggle-inline">' +
    '<span data-i18n="lib.showArchived">' +
    esc(t("lib.showArchived")) +
    "</span>" +
    '<span class="count-chip num" data-slot="archived-count">0</span>' +
    '<span class="switch sm">' +
    '<input type="checkbox" data-role="show-archived" data-i18n-aria="aria.showArchived" aria-label="' +
    esc(t("aria.showArchived")) +
    '" />' +
    '<span class="switch-track"></span>' +
    "</span>" +
    "</label>" +
    "</div>" +
    '<p class="label state-label" data-slot="state" role="status" hidden></p>' +
    '<div data-slot="error"></div>' +
    '<div class="list" data-slot="list" hidden></div>' +
    '<div class="empty-block" data-slot="empty" hidden>' +
    '<p class="empty-title" data-slot="empty-title"></p>' +
    '<p class="empty-note" data-slot="empty-note"></p>' +
    '<div class="empty-actions" data-slot="empty-actions" hidden>' +
    '<button type="button" class="btn" data-role="empty-add">' +
    core.icon("plus", "icon-sm") +
    '<span data-i18n="lib.add">' +
    esc(t("lib.add")) +
    "</span>" +
    "</button>" +
    "</div>" +
    "</div>" +
    "</div>";

  /* Small render helpers ------------------------------------------------- */

  function keywordMeta(item) {
    var count = typeof item.counter === "number" ? item.counter : 0;
    return '<span class="num">' + esc(num(count)) + "</span> " + esc(plural("unit.notices", count));
  }

  function addedMeta(item) {
    return esc(t("lib.added")) + ' <span class="num">' + esc(date(item.createdAt)) + "</span>";
  }

  function archivedMeta(item) {
    return esc(t("lib.archivedAt")) + ' <span class="num">' + esc(date(item.archivedAt)) + "</span>";
  }

  function rowMetaHTML(item) {
    if (item.archivedAt) {
      return archivedMeta(item);
    }
    return state.type === "keywords" ? keywordMeta(item) : addedMeta(item);
  }

  function rowActionsHTML(item, readOnly) {
    if (readOnly) {
      return "";
    }
    var id = esc(String(item.id));
    if (item.archivedAt) {
      return (
        '<div class="row-actions">' +
        '<button type="button" class="icon-btn" data-action="restore" data-id="' +
        id +
        '" data-i18n-aria="aria.restore" aria-label="' +
        esc(t("aria.restore")) +
        '">' +
        core.icon("undo", "icon-sm") +
        "</button>" +
        '<button type="button" class="icon-btn is-danger" data-action="delete" data-id="' +
        id +
        '" data-i18n-aria="aria.delete" aria-label="' +
        esc(t("aria.delete")) +
        '">' +
        core.icon("trash", "icon-sm") +
        "</button>" +
        "</div>"
      );
    }
    return (
      '<div class="row-actions">' +
      '<button type="button" class="icon-btn" data-action="archive" data-id="' +
      id +
      '" data-i18n-aria="aria.archive" aria-label="' +
      esc(t("aria.archive")) +
      '">' +
      core.icon("archive", "icon-sm") +
      "</button>" +
      '<button type="button" class="icon-btn" data-action="rename" data-id="' +
      id +
      '" data-i18n-aria="aria.rename" aria-label="' +
      esc(t("aria.rename")) +
      '">' +
      core.icon("edit", "icon-sm") +
      "</button>" +
      '<button type="button" class="icon-btn is-danger" data-action="delete" data-id="' +
      id +
      '" data-i18n-aria="aria.delete" aria-label="' +
      esc(t("aria.delete")) +
      '">' +
      core.icon("trash", "icon-sm") +
      "</button>" +
      "</div>"
    );
  }

  function rowHTML(item, readOnly) {
    return (
      '<div class="row' +
      (item.archivedAt ? " is-archived" : "") +
      '" data-id="' +
      esc(String(item.id)) +
      '">' +
      '<div class="row-main">' +
      '<span class="row-lead" title="' +
      esc(item.keyword) +
      '">' +
      esc(item.keyword) +
      "</span>" +
      '<span class="row-meta" data-role="meta">' +
      rowMetaHTML(item) +
      "</span>" +
      "</div>" +
      rowActionsHTML(item, readOnly) +
      "</div>"
    );
  }

  function filteredItems() {
    var needle = state.q.trim().toLowerCase();
    if (!needle) {
      return state.items;
    }
    return state.items.filter(function (item) {
      return item.keyword.toLowerCase().indexOf(needle) !== -1;
    });
  }

  function activeItems() {
    return filteredItems().filter(function (item) {
      return !item.archivedAt;
    });
  }

  function archivedItems() {
    return filteredItems()
      .filter(function (item) {
        return Boolean(item.archivedAt);
      })
      .sort(function (a, b) {
        return String(b.archivedAt).localeCompare(String(a.archivedAt));
      });
  }

  function findItem(id) {
    var wanted = String(id);
    for (var i = 0; i < state.items.length; i++) {
      if (String(state.items[i].id) === wanted) {
        return state.items[i];
      }
    }
    return null;
  }

  function activeTotal() {
    if (state.stats) {
      var keywords = Number(state.stats.keywords) || 0;
      var alarms = Number(state.stats.alarmKeywords) || 0;
      return keywords + alarms;
    }
    return null;
  }

  function viewingCount() {
    var viewing = state.viewing;
    if (!viewing || !viewing.keywordCounts) {
      return null;
    }
    var project = Number(viewing.keywordCounts.project) || 0;
    var alarm = Number(viewing.keywordCounts.alarm) || 0;
    return project + alarm;
  }

  /* Render --------------------------------------------------------------- */

  function renderSub() {
    if (state.viewing) {
      var count = viewingCount();
      var lastSeen = state.viewing.lastSeen ? i18n.monthDay(state.viewing.lastSeen) : "—";
      els.sub.textContent = t("lib.roSub", {
        name: core.identity(state.viewing).name,
        date: lastSeen,
        count: count === null ? "—" : num(count) + " " + plural("unit.keywords", count),
      });
      return;
    }

    var total = activeTotal();
    els.sub.textContent = "";
    if (total === null) {
      var placeholder = document.createElement("span");
      placeholder.className = "sk sk-inline";
      placeholder.setAttribute("aria-hidden", "true");
      els.sub.appendChild(placeholder);
      return;
    }
    var value = document.createElement("span");
    value.className = "num";
    value.textContent = num(total);
    els.sub.appendChild(value);
    els.sub.appendChild(
      document.createTextNode(" " + t(state.type === "alarms" ? "lib.subAlarms" : "lib.sub"))
    );
  }

  function renderBanner() {
    if (!state.viewing) {
      els.robar.hidden = true;
      els.robar.innerHTML = "";
      return;
    }
    var user = state.viewing;
    var ident = core.identity(user);
    els.robar.hidden = false;
    els.robar.className = "ro-bar";
    els.robar.setAttribute("role", "status");
    els.robar.innerHTML =
      core.icon("lock", "icon-sm") +
      "<span>" +
      esc(t("lib.roViewing")) +
      "</span>" +
      "<b>" +
      esc(ident.name) +
      "</b>" +
      ' <span class="ro-id num">' +
      esc(ident.idLabel) +
      "</span>" +
      ' <span class="ro-role">' +
      esc(roleLabel(user)) +
      "</span>" +
      " <span>" +
      esc(t("lib.roReadonly")) +
      "</span>" +
      '<button type="button" data-role="ro-exit" data-i18n-aria="aria.exit" aria-label="' +
      esc(t("aria.exit")) +
      '">' +
      esc(t("lib.roExit")) +
      "</button>";
    var exit = els.robar.querySelector('[data-role="ro-exit"]');
    if (exit) {
      exit.addEventListener("click", exitReadOnly);
    }
  }

  function renderSegments() {
    Array.prototype.forEach.call(els.segmented.querySelectorAll("button[data-type]"), function (button) {
      var active = button.getAttribute("data-type") === state.type;
      button.classList.toggle("is-active", active);
      button.setAttribute("aria-pressed", active ? "true" : "false");
    });
    els.search.setAttribute("placeholder", t(meta().searchPh));
    els.search.setAttribute("aria-label", t(meta().searchPh));
  }

  function renderHeader() {
    // The member picker stays reachable while viewing someone else: an
    // administrator switches targets from the same head action (design frame 4).
    els.members.hidden = !state.me || state.me.role !== "admin";
    els.add.hidden = Boolean(state.viewing);
    els.listhead.hidden = state.status !== "ready";
  }

  function renderList() {
    var readOnly = Boolean(state.viewing);
    var active = activeItems();
    var archived = archivedItems();

    els.list.innerHTML = "";
    els.list.hidden = active.length === 0 && archived.length === 0;
    els.list.setAttribute("data-role", "list");

    if (active.length > 0) {
      els.list.insertAdjacentHTML(
        "beforeend",
        active
          .map(function (item) {
            return rowHTML(item, readOnly);
          })
          .join("")
      );
    }

    if (state.showArchived && archived.length > 0) {
      var group = document.createElement("div");
      group.className = "group-label";
      group.innerHTML =
        core.icon("archive", "icon-sm") +
        "<span>" +
        esc(t("lib.archivedGroup")) +
        "</span>" +
        '<span class="count-chip num">' +
        esc(num(archived.length)) +
        "</span>" +
        '<span class="is-end">' +
        esc(t("lib.archivedHint")) +
        "</span>";
      els.list.appendChild(group);
      els.list.insertAdjacentHTML(
        "beforeend",
        archived
          .map(function (item) {
            return rowHTML(item, readOnly);
          })
          .join("")
      );
    }

    els.count.textContent = num(active.length) + " " + plural(meta().unit, active.length);
    els.archivedCount.textContent = num(state.archivedCount);
    els.showArchived.checked = state.showArchived;
  }

  function renderEmpty() {
    var showEmpty = state.status === "ready" && els.list.hidden;
    els.empty.hidden = !showEmpty;
    if (!showEmpty) {
      return;
    }

    var searching = state.q.trim() !== "";
    var noneAtAll = state.items.length === 0;
    els.emptyTitle.textContent = searching
      ? t(meta().emptyMatch, { q: state.q.trim() })
      : t(meta().emptyTitle);
    els.emptyNote.textContent = searching || !noneAtAll ? "" : t(meta().emptyNote);
    els.emptyNote.hidden = els.emptyNote.textContent === "";
    // A read-only view has no add path; the empty state must not offer one.
    els.emptyActions.hidden = searching || Boolean(state.viewing);
  }

  function renderStates() {
    var firstLoad = state.status === "loading" && state.items.length === 0;
    els.state.hidden = !firstLoad;
    if (firstLoad) {
      els.state.textContent = t("state.loading");
    }
    els.error.innerHTML = "";

    if (state.status === "error") {
      var wrap = document.createElement("div");
      wrap.className = "inline-error";
      wrap.setAttribute("role", "alert");
      wrap.innerHTML =
        core.icon("alert", "icon-sm") +
        '<div class="msg"><span>' +
        esc(t("lib.refreshFailed")) +
        "</span>" +
        (state.items.length > 0 && state.loadedAt
          ? '<span class="sub">' + esc(t("lib.stillShowing", { time: i18n.time(state.loadedAt) })) + "</span>"
          : "") +
        "</div>";
      var retry = document.createElement("button");
      retry.type = "button";
      retry.className = "btn-ghost btn-sm";
      retry.textContent = t("lib.retry");
      retry.addEventListener("click", function () {
        load();
      });
      wrap.appendChild(retry);
      els.error.appendChild(wrap);
    }
  }

  function renderSkeleton() {
    var blocks = "";
    for (var i = 0; i < 4; i++) {
      blocks +=
        '<div class="sk-block" aria-hidden="true"><span class="sk ' +
        (i % 2 ? "w40" : "w60") +
        '"></span><span class="sk w40"></span></div>';
    }
    els.list.innerHTML = blocks;
    els.list.hidden = false;
    els.list.removeAttribute("data-role");
  }

  function renderAll() {
    renderBanner();
    renderSub();
    renderSegments();
    renderHeader();
    renderStates();
    if (state.status === "loading" && state.items.length === 0) {
      renderSkeleton();
      els.empty.hidden = true;
      els.listhead.hidden = true;
      return;
    }
    renderList();
    renderEmpty();
  }

  /* Dialog --------------------------------------------------------------- */

  function closeDialog() {
    if (!dialog) {
      return;
    }
    if (dialog.el.parentNode) {
      dialog.el.parentNode.removeChild(dialog.el);
    }
    if (dialog.scrim.parentNode) {
      dialog.scrim.parentNode.removeChild(dialog.scrim);
    }
    var last = dialog.lastFocus;
    dialog = null;
    if (last && typeof last.focus === "function") {
      last.focus();
    }
  }

  function dialogLabels() {
    if (!dialog) {
      return;
    }
    dialog.title.textContent = t(dialog.titleKey);
    dialog.kicker.textContent = dialog.kickerKey ? t(dialog.kickerKey) : "";
    dialog.kicker.hidden = !dialog.kickerKey;
    dialog.confirm.textContent = t(dialog.pending ? dialog.pendingKey : dialog.confirmKey);
    dialog.cancel.textContent = t("common.cancel");
    if (i18n) {
      i18n.apply(dialog.el);
    }
    var close = dialog.el.querySelector('[data-role="close"]');
    if (close) {
      close.setAttribute("aria-label", t("aria.close"));
    }
  }

  function setDialogPending(pending) {
    if (!dialog) {
      return;
    }
    dialog.pending = pending;
    dialog.confirm.disabled = pending || Boolean(dialog.empty);
    dialog.cancel.disabled = pending;
    dialog.el.querySelector('[data-role="close"]').disabled = pending;
    dialogLabels();
  }

  function refreshConfirmState() {
    if (!dialog) {
      return;
    }
    dialog.empty = dialog.field ? dialog.field.value.trim() === "" : false;
    dialog.confirm.disabled = dialog.pending || dialog.empty;
  }

  /**
   * Opens the bottom sheet used by every library write. The caller supplies the
   * body node and the confirm behaviour; labels are keys so a language switch
   * while the sheet is open updates text without closing it.
   */
  function openDialog(config) {
    closeDialog();

    var scrim = document.createElement("div");
    scrim.className = "scrim";

    var sheet = document.createElement("section");
    sheet.className = "sheet";
    sheet.setAttribute("role", "dialog");
    sheet.setAttribute("aria-modal", "true");
    sheet.innerHTML =
      '<div class="sheet-grab" aria-hidden="true"></div>' +
      '<div class="sheet-head"><div>' +
      '<h3 class="sheet-title" data-role="title"></h3>' +
      '<p class="sheet-kicker" data-role="kicker"></p>' +
      "</div>" +
      '<button type="button" class="icon-btn" data-role="close">' +
      core.icon("close") +
      "</button></div>" +
      '<div class="sheet-body" data-role="body"></div>' +
      '<div class="sheet-actions">' +
      '<button type="button" class="btn-ghost is-grow" data-role="cancel"></button>' +
      '<button type="button" class="btn is-grow" data-role="confirm"></button>' +
      "</div>";

    document.body.appendChild(scrim);
    document.body.appendChild(sheet);

    dialog = {
      el: sheet,
      scrim: scrim,
      title: sheet.querySelector('[data-role="title"]'),
      kicker: sheet.querySelector('[data-role="kicker"]'),
      body: sheet.querySelector('[data-role="body"]'),
      confirm: sheet.querySelector('[data-role="confirm"]'),
      cancel: sheet.querySelector('[data-role="cancel"]'),
      field: config.field || null,
      lastFocus: document.activeElement,
      titleKey: config.titleKey,
      kickerKey: config.kickerKey || "",
      confirmKey: config.confirmKey || "common.save",
      pendingKey: config.pendingKey || "common.saving",
      pending: false,
      empty: false,
      onConfirm: config.onConfirm,
    };

    if (config.danger) {
      dialog.confirm.classList.add("btn-danger");
    }

    dialog.body.appendChild(config.content);
    dialogLabels();
    refreshConfirmState();

    sheet.querySelector('[data-role="close"]').addEventListener("click", closeDialog);
    dialog.cancel.addEventListener("click", closeDialog);
    dialog.confirm.addEventListener("click", confirmDialog);
    scrim.addEventListener("click", closeDialog);

    // Cancel is the safe default for a destructive confirmation.
    var focusTarget = config.focus || (config.danger ? dialog.cancel : dialog.confirm);
    if (focusTarget && typeof focusTarget.focus === "function") {
      focusTarget.focus();
    }
    if (dialog.field && dialog.field.tagName === "TEXTAREA") {
      dialog.field.focus();
    }
  }

  function confirmDialog() {
    if (!dialog || dialog.pending || dialog.empty || typeof dialog.onConfirm !== "function") {
      return;
    }
    var pending;
    try {
      pending = dialog.onConfirm(dialog.field);
    } catch (err) {
      handleWriteError(err);
      return;
    }
    if (!pending || typeof pending.then !== "function") {
      return;
    }
    setDialogPending(true);
    pending.then(
      function () {
        setDialogPending(false);
      },
      function () {
        setDialogPending(false);
      }
    );
  }

  function onDialogKeydown(event) {
    if (!dialog) {
      return;
    }
    var key = event.key || "";
    if (key === "Escape" || key === "Esc") {
      event.preventDefault();
      closeDialog();
      return;
    }
    if (key === "Enter" && dialog.field && dialog.field.tagName === "INPUT" && !dialog.pending) {
      event.preventDefault();
      // An IME uses Enter to confirm a candidate, not to submit the field.
      if (event.isComposing || event.keyCode === 229) {
        return;
      }
      confirmDialog();
    }
  }

  /* Write actions -------------------------------------------------------- */

  function handleWriteError(err) {
    var status = err && err.status;
    if (status === 429) {
      core.toast(t("app.rateLimited"), "error");
      return;
    }
    if (status === 404) {
      closeDialog();
      core.toast(t("app.error"), "error");
      load();
      return;
    }
    core.toast((err && err.message) || t("app.error"), "error");
  }

  function invalidateWrites() {
    core.state.invalidate("today");
    core.state.invalidate("members");
    core.state.invalidate("admin");
    state.stats = null;
    loadStats();
  }

  function loadStats() {
    if (state.viewing) {
      renderSub();
      return;
    }
    api()
      .statistics()
      .then(function (payload) {
        state.stats = payload || {};
        renderSub();
      })
      .catch(function () {
        // The summary is decorative; the list is the task.
      });
  }

  function setArchived(item, archived, options) {
    var previous = item.archivedAt;
    var index = state.items.indexOf(item);
    if (index === -1) {
      return Promise.resolve();
    }

    // Optimistic: the row moves now so the undo toast can name what changed.
    item.archivedAt = archived ? new Date().toISOString() : null;
    state.archivedCount = Math.max(0, state.archivedCount + (archived ? 1 : -1));
    renderAll();

    var request = api()[meta().update](item.id, { archived: archived });
    return request.then(
      function (payload) {
        item.archivedAt = payload && payload.archivedAt ? payload.archivedAt : item.archivedAt;
        invalidateWrites();
        renderAll();
        if (archived) {
          core.toast(t("lib.archivedToast", { name: item.keyword }), "info", {
            actionLabel: t("common.undo"),
            timeout: 5000,
            onAction: function () {
              setArchived(item, false, { silent: true }).then(function () {
                core.toast(t("lib.restoredToast", { name: item.keyword }), "success");
              });
            },
          });
        } else if (!options || !options.silent) {
          core.toast(t("lib.restoredToast", { name: item.keyword }), "success");
        }
      },
      function (err) {
        item.archivedAt = previous;
        state.archivedCount = Math.max(0, state.archivedCount + (archived ? -1 : 1));
        renderAll();
        handleWriteError(err);
      }
    );
  }

  function openAddDialog() {
    if (state.viewing) {
      return;
    }
    var textarea = document.createElement("textarea");
    textarea.className = "dialog-textarea";
    textarea.rows = 3;
    textarea.placeholder = t("lib.addHint");
    textarea.setAttribute("data-i18n-ph", "lib.addHint");
    textarea.setAttribute("aria-label", t("lib.addHint"));
    textarea.addEventListener("input", refreshConfirmState);

    openDialog({
      titleKey: meta().addTitle,
      confirmKey: "common.add",
      pendingKey: "common.adding",
      content: textarea,
      field: textarea,
      focus: textarea,
      onConfirm: function () {
        var raw = textarea.value.trim();
        if (!raw) {
          return null;
        }
        return api()
          [meta().create](raw)
          .then(function (payload) {
            var added = payload && Array.isArray(payload.added) ? payload.added : [];
            var skipped = payload && Array.isArray(payload.skipped) ? payload.skipped : [];
            core.toast(
              skipped.length
                ? t("lib.addedSkippedToast", { count: num(added.length), skipped: num(skipped.length) })
                : t("lib.addedToast", { count: num(added.length) }),
              "success"
            );
            closeDialog();
            invalidateWrites();
            return load();
          })
          .catch(function (err) {
            handleWriteError(err);
          });
      },
    });
  }

  function openRenameDialog(item) {
    var input = document.createElement("input");
    input.type = "text";
    input.className = "dialog-input";
    input.value = item.keyword;
    input.maxLength = MAX_KEYWORD_LENGTH;
    input.setAttribute("aria-label", t("aria.keyword"));
    input.setAttribute("data-i18n-aria", "aria.keyword");
    input.addEventListener("input", refreshConfirmState);

    openDialog({
      titleKey: meta().renameTitle,
      confirmKey: "common.save",
      content: input,
      field: input,
      focus: input,
      onConfirm: function () {
        var next = input.value.trim();
        if (!next) {
          return null;
        }
        return api()
          [meta().update](item.id, { keyword: next })
          .then(function (payload) {
            item.keyword = payload && typeof payload.keyword === "string" ? payload.keyword : next;
            closeDialog();
            invalidateWrites();
            renderAll();
            core.toast(t("lib.renamedToast"), "success");
          })
          .catch(function (err) {
            handleWriteError(err);
          });
      },
    });
  }

  function openDeleteDialog(item) {
    var message = document.createElement("div");
    var name = document.createElement("p");
    name.className = "field-label";
    name.textContent = item.keyword;
    var note = document.createElement("p");
    note.className = "dialog-message";
    note.textContent = t("lib.deleteBody");
    message.appendChild(name);
    message.appendChild(note);

    openDialog({
      titleKey: meta().deleteTitle,
      confirmKey: "common.delete",
      pendingKey: "common.deleting",
      content: message,
      danger: true,
      focus: null,
      onConfirm: function () {
        var removal =
          state.type === "keywords" ? api().deleteKeyword(item.id) : api().deleteAlarm(item.id);
        return removal
          .then(function () {
            removeItem(item);
            closeDialog();
            invalidateWrites();
            renderAll();
            core.toast(t("lib.deletedToast"), "success");
          })
          .catch(function (err) {
            handleWriteError(err);
          });
      },
    });
  }

  function removeItem(item) {
    var index = state.items.indexOf(item);
    if (index !== -1) {
      if (item.archivedAt) {
        state.archivedCount = Math.max(0, state.archivedCount - 1);
      }
      state.items.splice(index, 1);
    }
  }

  /* Data ----------------------------------------------------------------- */

  function normalize(payload) {
    var raw = payload && Array.isArray(payload.items) ? payload.items : [];
    var items = [];
    for (var i = 0; i < raw.length; i++) {
      var row = raw[i];
      if (!row || typeof row !== "object") {
        continue;
      }
      items.push({
        id: row.id,
        keyword: typeof row.keyword === "string" ? row.keyword : "",
        counter: typeof row.counter === "number" && isFinite(row.counter) ? row.counter : 0,
        createdAt: row.createdAt || "",
        archivedAt: row.archivedAt || null,
      });
    }
    return {
      items: items,
      archivedCount:
        typeof payload.archivedCount === "number" && isFinite(payload.archivedCount)
          ? payload.archivedCount
          : items.filter(function (item) {
              return item.archivedAt;
            }).length,
    };
  }

  function load() {
    if (state.viewing) {
      return loadAdmin();
    }
    state.status = "loading";
    renderAll();

    return api()
      [meta().api]({ includeArchived: 1 })
      .then(function (payload) {
        var data = normalize(payload);
        state.items = data.items;
        state.archivedCount = data.archivedCount;
        state.status = "ready";
        state.loaded = true;
        state.loadedAt = new Date().toISOString();
        renderAll();
        restoreScroll();
        loadStats();
      })
      .catch(function () {
        state.status = "error";
        state.failedAt = new Date().toISOString();
        renderAll();
      });
  }

  function loadAdmin() {
    var viewing = state.viewing;
    if (!viewing) {
      return load();
    }
    state.status = "loading";
    renderAll();

    return api()
      .adminUserKeywords(viewing.userId, {
        type: meta().adminType,
        includeArchived: 1,
      })
      .then(function (payload) {
        var data = normalize(payload);
        state.items = data.items;
        state.archivedCount = data.archivedCount;
        if (payload && payload.user) {
          state.viewing = payload.user;
          core.state.viewingUser = payload.user;
        }
        state.status = "ready";
        state.loaded = true;
        state.loadedAt = new Date().toISOString();
        renderAll();
        restoreScroll();
        loadStats();
      })
      .catch(function (err) {
        state.status = "error";
        state.failedAt = new Date().toISOString();
        renderAll();
        if (err && err.status === 404) {
          core.toast(t("app.error"), "error");
        }
      });
  }

  function restoreScroll() {
    if (!els || !state.scrollTop) {
      return;
    }
    els.screen.scrollTop = state.scrollTop;
  }

  function exitReadOnly() {
    state.viewing = null;
    core.state.viewingUser = null;
    state.items = [];
    state.archivedCount = 0;
    state.loaded = false;
    state.status = "idle";
    state.scrollTop = 0;
    load();
  }

  function enterReadOnly(user) {
    state.viewing = user;
    state.items = [];
    state.archivedCount = 0;
    state.loaded = false;
    state.status = "idle";
    state.scrollTop = 0;
    load();
  }

  /* Wiring --------------------------------------------------------------- */

  function build(section) {
    section.innerHTML = TEMPLATE;
    els = {
      screen: section.querySelector('[data-role="screen"]'),
      robar: section.querySelector('[data-slot="robar"]'),
      sub: section.querySelector('[data-slot="sub"]'),
      members: section.querySelector('[data-role="members"]'),
      segmented: section.querySelector('[data-slot="segmented"]'),
      search: section.querySelector('[data-role="search"]'),
      add: section.querySelector('[data-role="add"]'),
      listhead: section.querySelector('[data-slot="listhead"]'),
      count: section.querySelector('[data-slot="count"]'),
      archivedCount: section.querySelector('[data-slot="archived-count"]'),
      showArchived: section.querySelector('[data-role="show-archived"]'),
      state: section.querySelector('[data-slot="state"]'),
      error: section.querySelector('[data-slot="error"]'),
      list: section.querySelector('[data-slot="list"]'),
      empty: section.querySelector('[data-slot="empty"]'),
      emptyTitle: section.querySelector('[data-slot="empty-title"]'),
      emptyNote: section.querySelector('[data-slot="empty-note"]'),
      emptyActions: section.querySelector('[data-slot="empty-actions"]'),
    };
  }

  function bind() {
    core.bindRefresh(els.screen, function () {
      load();
    });

    els.segmented.addEventListener("click", function (event) {
      var button = event.target.closest("button[data-type]");
      if (!button || button.getAttribute("data-type") === state.type) {
        return;
      }
      state.type = button.getAttribute("data-type");
      state.items = [];
      state.archivedCount = 0;
      state.loaded = false;
      load();
    });

    els.search.addEventListener("input", function () {
      state.q = els.search.value;
      renderList();
      renderEmpty();
    });

    els.showArchived.addEventListener("change", function () {
      state.showArchived = els.showArchived.checked;
      storeShowArchived(state.showArchived);
      renderList();
      renderEmpty();
    });

    els.add.addEventListener("click", openAddDialog);
    els.emptyActions.querySelector('[data-role="empty-add"]').addEventListener("click", openAddDialog);
    els.members.addEventListener("click", function () {
      core.go("members");
    });

    els.list.addEventListener("click", function (event) {
      var button = event.target.closest("button[data-action]");
      if (!button) {
        return;
      }
      var item = findItem(button.getAttribute("data-id"));
      if (!item) {
        return;
      }
      var action = button.getAttribute("data-action");
      if (action === "archive") {
        setArchived(item, true);
      } else if (action === "restore") {
        setArchived(item, false);
      } else if (action === "rename") {
        openRenameDialog(item);
      } else if (action === "delete") {
        openDeleteDialog(item);
      }
    });

    els.screen.addEventListener("scroll", function () {
      state.scrollTop = els.screen.scrollTop;
    });

    document.addEventListener("keydown", onDialogKeydown);
  }

  function onLang() {
    if (!els) {
      return;
    }
    renderBanner();
    renderSub();
    renderSegments();
    renderHeader();
    renderStates();
    refreshRowText();
    renderEmpty();
    dialogLabels();
  }

  // The list owns user data and async state, so a language switch must not
  // rebuild it: only the meta line and the accessible names change. Row
  // elements stay connected, which keeps focus and the scroll position.
  function refreshRowText() {
    var rows = els.list.querySelectorAll(".row[data-id]");
    Array.prototype.forEach.call(rows, function (row) {
      var item = findItem(row.getAttribute("data-id"));
      if (!item) {
        return;
      }
      var metaEl = row.querySelector('[data-role="meta"]');
      if (metaEl) {
        metaEl.innerHTML = rowMetaHTML(item);
      }
      Array.prototype.forEach.call(row.querySelectorAll("button[data-i18n-aria]"), function (button) {
        button.setAttribute("aria-label", t(button.getAttribute("data-i18n-aria")));
      });
    });

    var group = els.list.querySelector(".group-label");
    if (group) {
      var spans = group.querySelectorAll("span");
      if (spans[0]) {
        spans[0].textContent = t("lib.archivedGroup");
      }
      if (spans.length > 1) {
        spans[spans.length - 1].textContent = t("lib.archivedHint");
      }
    }

    var active = activeItems();
    els.count.textContent = num(active.length) + " " + plural(meta().unit, active.length);
    els.archivedCount.textContent = num(state.archivedCount);
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

    var viewing = core.state.viewingUser;
    if (viewing && (!state.viewing || state.viewing.userId !== viewing.userId)) {
      enterReadOnly(viewing);
      return Promise.resolve();
    }
    if (!viewing && state.viewing) {
      exitReadOnly();
      return Promise.resolve();
    }

    core
      .me()
      .then(function (me) {
        state.me = me;
        renderHeader();
      })
      .catch(function () {
        state.me = null;
        renderHeader();
      });

    if (!state.loaded || core.state.isStale(VIEW)) {
      return load();
    }
    renderAll();
    restoreScroll();
    return Promise.resolve();
  }

  core.registerView(VIEW, { mount: mount, onLang: onLang });
})(window);
