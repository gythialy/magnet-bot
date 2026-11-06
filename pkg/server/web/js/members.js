/*
 * members.js — the administrator's member picker page.
 *
 * The picker is a page, not a sheet: a sticky search, the caller pinned on top,
 * recently active members first, a counted pager, and disabled accounts folded
 * into one row at the bottom. Picking a row only sets the read-only target:
 * library.js owns what that target sees and never lets this page write.
 *
 * Rules this file owns:
 *   - only administrators reach the list; a non-admin sees the admin-only panel
 *   - search is server-side (q) and debounced; the result count never hides
 *   - a failed refresh keeps the rows it had and offers one retry; a failed
 *     "load more" leaves the paging cursor where it was
 *   - switching language rewrites text in place, so the list, the search
 *     draft, focus and the scroll position survive
 */
(function (global) {
  "use strict";

  var core = global.MagnetApp;
  if (!core) {
    return;
  }

  var i18n = global.MagnetI18n;
  var VIEW = "members";
  var SECTION_ID = "view-members";
  var PAGE_SIZE = 20;
  var SEARCH_DEBOUNCE_MS = 250;
  // A small request tells the collapsed row its count without loading rows.
  // Several rows, so one disabled top hit cannot pass an ignored filter off as
  // a supported one.
  var DISABLED_PROBE_LIMIT = 5;

  var state = {
    gate: "checking", // checking | ready | denied | error
    me: null,
    mePromise: null,
    q: "",
    pinned: null,
    items: [],
    total: 0,
    fetched: 0,
    nextOffset: null,
    status: "idle", // idle | loading | ready | error
    loaded: false,
    loadedAt: null,
    loadingMore: false,
    pageError: false,
    seq: 0,
    pinnedSeq: 0,
    scrollTop: 0,
    disabled: freshDisabled(),
  };

  var sectionEl = null;
  var els = null;

  function freshDisabled() {
    return {
      mode: "unknown", // unknown | server | client
      total: null,
      expanded: false,
      loaded: false,
      loading: false,
      error: false,
      items: [],
      fetched: 0,
      nextOffset: null,
      seq: 0,
      probeSeq: 0,
    };
  }

  /* Helpers --------------------------------------------------------------- */

  function t(key, vars) {
    return i18n ? i18n.t(key, vars) : key;
  }

  function plural(key, count) {
    return i18n ? i18n.plural(key, count) : key;
  }

  function num(value) {
    return i18n ? i18n.num(value) : String(value);
  }

  function time(value) {
    return i18n ? i18n.time(value) : "";
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
    return typeof value === "number" && isFinite(value) ? value : 0;
  }

  function nextOffsetOf(payload, returned) {
    if (!returned) {
      return null;
    }
    var next = payload && payload.nextOffset;
    return typeof next === "number" && isFinite(next) ? next : null;
  }

  function normalizeItem(row) {
    if (!row || typeof row !== "object") {
      return null;
    }
    var counts = row.keywordCounts;
    return {
      userId: row.userId,
      remark: typeof row.remark === "string" ? row.remark : "",
      name: typeof row.name === "string" ? row.name : "",
      username: typeof row.username === "string" ? row.username : "",
      role: row.role === "admin" ? "admin" : "user",
      enabled: row.enabled !== false,
      fromEnv: row.fromEnv === true,
      createdAt: row.createdAt || "",
      lastSeen: row.lastSeen || null,
      keywordCounts:
        counts && typeof counts === "object"
          ? { project: numberOrZero(counts.project), alarm: numberOrZero(counts.alarm) }
          : null,
    };
  }

  function normalize(payload) {
    var raw = payload && Array.isArray(payload.items) ? payload.items : [];
    var items = [];
    for (var i = 0; i < raw.length; i++) {
      var item = normalizeItem(raw[i]);
      if (item) {
        items.push(item);
      }
    }
    return items;
  }

  function itemFromMe(me) {
    return {
      userId: me.userId,
      remark: me.remark || "",
      name: me.name || "",
      username: me.username || "",
      role: me.role === "admin" ? "admin" : "user",
      enabled: true,
      fromEnv: false,
      createdAt: "",
      lastSeen: null,
      keywordCounts: null,
    };
  }

  function keywordTotal(item) {
    if (!item || !item.keywordCounts) {
      return null;
    }
    return numberOrZero(item.keywordCounts.project) + numberOrZero(item.keywordCounts.alarm);
  }

  function resultText() {
    return num(state.total) + " " + plural("unit.results", state.total);
  }

  function remainingCount() {
    var left = state.total - state.fetched;
    return left > 0 ? left : 0;
  }

  function disabledRemaining() {
    var left = numberOrZero(state.disabled.total) - state.disabled.fetched;
    return left > 0 ? left : 0;
  }

  function groupCount() {
    // The active page always includes the pinned caller, so remove that one row
    // from the group count; the row itself is rendered on top instead.
    return Math.max(0, state.total - (state.me ? 1 : 0));
  }

  function findUser(id) {
    var wanted = String(id);
    if (state.pinned && String(state.pinned.userId) === wanted) {
      return state.pinned;
    }
    var pools = [state.items, state.disabled.items];
    for (var p = 0; p < pools.length; p++) {
      for (var i = 0; i < pools[p].length; i++) {
        if (String(pools[p][i].userId) === wanted) {
          return pools[p][i];
        }
      }
    }
    return null;
  }

  /* Template -------------------------------------------------------------- */

  function template() {
    return (
      '<div class="screen members-screen" data-role="screen">' +
      core.subTopbarHTML("pick.title") +
      '<div class="members-body" data-slot="body">' +
      '<p class="pick-hint" data-i18n="pick.hint">' +
      esc(t("pick.hint")) +
      "</p>" +
      '<div class="members-search">' +
      '<label class="search">' +
      core.icon("search", "icon-sm") +
      '<input type="search" data-role="search" autocomplete="off" spellcheck="false" enterkeyhint="search" ' +
      'data-i18n-ph="pick.searchPh" data-i18n-aria="pick.searchPh" ' +
      'placeholder="' +
      esc(t("pick.searchPh")) +
      '" aria-label="' +
      esc(t("pick.searchPh")) +
      '" />' +
      '<button type="button" class="search-clear" data-role="clear" data-i18n-aria="aria.clear" aria-label="' +
      esc(t("aria.clear")) +
      '" hidden>' +
      core.icon("close", "icon-sm") +
      "</button>" +
      "</label>" +
      "</div>" +
      '<p class="result-note" data-slot="result" role="status" hidden></p>' +
      '<div data-slot="error" hidden></div>' +
      '<div class="pick-list" data-slot="list"></div>' +
      '<div class="empty-block" data-slot="empty" hidden>' +
      '<p class="empty-title" data-slot="empty-title"></p>' +
      '<div class="empty-actions">' +
      '<button type="button" class="btn-ghost" data-role="clear-search" data-i18n="pick.clearSearch">' +
      esc(t("pick.clearSearch")) +
      "</button>" +
      "</div>" +
      "</div>" +
      '<div data-slot="foot" hidden></div>' +
      "</div>" +
      '<div data-slot="gate" hidden></div>' +
      "</div>"
    );
  }

  function skeletonHTML(count) {
    var rows = "";
    for (var i = 0; i < count; i++) {
      rows +=
        '<div class="members-sk" aria-hidden="true">' +
        '<span class="members-sk-avatar"></span>' +
        '<span class="members-sk-lines"><span class="sk w60"></span><span class="sk w40"></span></span>' +
        "</div>";
    }
    return rows;
  }

  function rowHTML(item, options) {
    var opts = options || {};
    var ident = core.identity(item);
    var classes = "pick-row";
    if (opts.pinned) {
      classes += " is-pinned";
    }
    if (item.enabled === false) {
      classes += " is-disabled";
    }

    return (
      '<button type="button" class="' +
      classes +
      '" data-user-id="' +
      esc(item.userId) +
      '"' +
      (opts.pinned ? ' data-pinned="true"' : "") +
      ' aria-pressed="false">' +
      '<span class="avatar" aria-hidden="true">' +
      esc(ident.initial) +
      "</span>" +
      '<span class="pick-main">' +
      '<span class="pick-name">' +
      '<span data-slot="name"></span> ' +
      '<span class="pick-id num" data-slot="id" hidden></span> ' +
      '<span class="name-note" data-slot="note" hidden></span>' +
      "</span>" +
      '<span class="pick-meta">' +
      '<span data-slot="role"></span>' +
      '<span class="sep" data-slot="state-sep" hidden>·</span>' +
      '<span data-slot="state" hidden></span>' +
      '<span class="sep" data-slot="count-sep" hidden>·</span>' +
      '<span class="num" data-slot="count" hidden></span>' +
      '<span data-slot="unit" hidden></span>' +
      "</span>" +
      "</span>" +
      core.icon("check", "pick-check") +
      "</button>"
    );
  }

  function groupLabelHTML() {
    var count = groupCount();
    // When the API ignores the enabled filter the total still counts disabled
    // accounts; an honest group hides the number instead of showing a wrong one.
    var hideChip = state.disabled.mode === "client";
    return (
      '<div class="group-label">' +
      '<span data-i18n="pick.groupActive">' +
      esc(t("pick.groupActive")) +
      "</span>" +
      '<span class="count-chip num" data-slot="group-count"' +
      (hideChip ? " hidden" : "") +
      ">" +
      esc(num(count)) +
      "</span>" +
      '<span class="is-end" data-i18n="pick.sortNote">' +
      esc(t("pick.sortNote")) +
      "</span>" +
      "</div>"
    );
  }

  function inlineErrorHTML(message, sub, role) {
    return (
      '<div class="inline-error" role="alert">' +
      core.icon("alert", "icon-sm") +
      '<div class="msg"><span>' +
      esc(message) +
      "</span>" +
      (sub ? '<span class="sub">' + esc(sub) + "</span>" : "") +
      "</div>" +
      '<button type="button" class="btn-ghost btn-sm" data-role="' +
      esc(role) +
      '">' +
      esc(t("state.retry")) +
      "</button>" +
      "</div>"
    );
  }

  function disabledGroupVisible() {
    var disabled = state.disabled;
    if (!state.loaded) {
      return false;
    }
    if (disabled.mode === "server") {
      return disabled.expanded || numberOrZero(disabled.total) > 0;
    }
    if (disabled.mode === "client") {
      return disabled.expanded || disabled.items.length > 0 || !disabled.loaded;
    }
    return true;
  }

  function disabledGroupHTML() {
    if (!disabledGroupVisible()) {
      return "";
    }

    var disabled = state.disabled;
    var chip = "";
    if (disabled.mode === "server" && disabled.total !== null) {
      chip =
        '<span class="count-chip num" data-slot="disabled-count">' +
        esc(num(disabled.total)) +
        "</span>";
    }

    var html =
      '<button type="button" class="row is-link members-disabled-toggle" data-role="disabled-toggle" aria-expanded="' +
      (disabled.expanded ? "true" : "false") +
      '">' +
      '<span class="row-lead is-caption"><span data-i18n="pick.disabledGroup">' +
      esc(t("pick.disabledGroup")) +
      "</span> " +
      chip +
      "</span>" +
      core.icon("chevron") +
      "</button>";

    if (!disabled.expanded) {
      return html;
    }

    html += '<div class="members-disabled-list">';
    if (disabled.loading) {
      html += skeletonHTML(2);
    } else if (disabled.error) {
      html += inlineErrorHTML(t("pick.loadFailed"), "", "disabled-retry");
    } else if (disabled.items.length > 0) {
      html += disabled.items
        .map(function (item) {
          return rowHTML(item, {});
        })
        .join("");
      if (disabled.mode === "server" && disabled.nextOffset !== null) {
        html +=
          '<div class="list-foot is-tight"><button type="button" class="btn-wide" data-role="disabled-load-more">' +
          esc(t("pick.loadMore", { count: num(disabledRemaining()) })) +
          "</button></div>";
      }
    } else {
      html +=
        '<p class="pick-hint is-top" data-i18n="pick.disabledHint">' +
        esc(t("pick.disabledHint")) +
        "</p>";
    }
    html += "</div>";

    return html;
  }

  /* Rendering ------------------------------------------------------------- */

  function setSlotText(scope, slot, value) {
    var el = scope.querySelector('[data-slot="' + slot + '"]');
    if (el) {
      el.textContent = value;
    }
  }

  function updateRowText(button, item) {
    var ident = core.identity(item);
    setSlotText(button, "name", ident.name);

    var idEl = button.querySelector('[data-slot="id"]');
    idEl.textContent = ident.unnamed ? "" : "· " + ident.idLabel;
    idEl.hidden = ident.unnamed;

    var isMe = state.me && String(item.userId) === String(state.me.userId);
    var noteKey = isMe ? "set.you" : ident.unnamed ? "identity.unnamed" : "";
    var noteEl = button.querySelector('[data-slot="note"]');
    noteEl.textContent = noteKey ? t(noteKey) : "";
    noteEl.hidden = noteKey === "";

    setSlotText(button, "role", t(item.role === "admin" ? "role.admin" : "role.user"));

    // Disabled is a status, not a role: it sits next to the role in the meta
    // line and the row stays clickable, because inspection is read-only.
    var isDisabled = item.enabled === false;
    var stateEl = button.querySelector('[data-slot="state"]');
    stateEl.textContent = isDisabled ? t("set.disabled") : "";
    stateEl.hidden = !isDisabled;
    var stateSep = button.querySelector('[data-slot="state-sep"]');
    stateSep.hidden = !isDisabled;

    var total = keywordTotal(item);
    var known = total !== null;
    var count = known ? total : 0;
    var countEl = button.querySelector('[data-slot="count"]');
    countEl.textContent = known ? num(count) : "";
    countEl.hidden = !known;
    var unitEl = button.querySelector('[data-slot="unit"]');
    unitEl.textContent = known ? plural("unit.keywords", count) : "";
    unitEl.hidden = !known;
    var countSep = button.querySelector('[data-slot="count-sep"]');
    countSep.hidden = !known;
  }

  function applyRowTexts() {
    if (!sectionEl) {
      return;
    }
    var rows = sectionEl.querySelectorAll("button.pick-row[data-user-id]");
    Array.prototype.forEach.call(rows, function (button) {
      var item = findUser(button.getAttribute("data-user-id"));
      if (item) {
        updateRowText(button, item);
      }
    });

    var groupCountEl = sectionEl.querySelector('[data-slot="group-count"]');
    if (groupCountEl && state.loaded && state.q.trim() === "") {
      groupCountEl.textContent = num(groupCount());
    }
    var disabledCountEl = sectionEl.querySelector('[data-slot="disabled-count"]');
    if (disabledCountEl && state.disabled.mode === "server" && state.disabled.total !== null) {
      disabledCountEl.textContent = num(state.disabled.total);
    }
  }

  function syncSelection() {
    if (!sectionEl) {
      return;
    }
    var viewing = core.state.viewingUser;
    var viewingId = viewing ? String(viewing.userId) : "";
    var rows = sectionEl.querySelectorAll("button.pick-row[data-user-id]");
    Array.prototype.forEach.call(rows, function (button) {
      var id = button.getAttribute("data-user-id");
      // No target means the caller is inspecting their own library, so the
      // pinned "you" row carries the check.
      var active = viewingId !== "" ? id === viewingId : button.hasAttribute("data-pinned");
      button.classList.toggle("is-on", active);
      button.setAttribute("aria-pressed", active ? "true" : "false");
    });
  }

  function renderList(searching) {
    var html = "";
    if (!searching) {
      if (state.pinned) {
        html += rowHTML(state.pinned, { pinned: true });
      }
      if (state.loaded) {
        html += groupLabelHTML();
      }
    }
    html += state.items
      .map(function (item) {
        return rowHTML(item, {});
      })
      .join("");
    els.list.innerHTML = html;
    els.list.hidden = Boolean(searching) && state.items.length === 0;
  }

  function renderResult(searching) {
    if (!searching) {
      els.result.hidden = true;
      els.result.textContent = "";
      return;
    }
    els.result.hidden = false;
    els.result.textContent = resultText();
  }

  function renderEmpty(searching) {
    var show = Boolean(searching) && state.status === "ready" && state.items.length === 0;
    els.empty.hidden = !show;
    if (show) {
      els.emptyTitle.textContent = t("pick.noResults");
    }
  }

  function renderError() {
    if (state.status !== "error") {
      els.error.hidden = true;
      els.error.innerHTML = "";
      return;
    }
    // "Couldn't refresh" is only honest when a page was on screen before.
    var hadData = state.loaded;
    var sub = hadData && state.loadedAt ? t("state.failSub", { time: time(state.loadedAt) }) : "";
    els.error.hidden = false;
    els.error.innerHTML = inlineErrorHTML(
      hadData ? t("state.fail") : t("pick.loadFailed"),
      sub,
      "retry"
    );
  }

  function renderFoot(searching) {
    els.foot.innerHTML = "";
    els.foot.hidden = Boolean(searching);
    if (searching) {
      return;
    }

    var html = "";
    if (state.loaded) {
      if (state.nextOffset !== null && remainingCount() > 0) {
        html +=
          '<div class="list-foot"><button type="button" class="btn-wide" data-role="load-more"' +
          (state.loadingMore ? " disabled" : "") +
          ">" +
          esc(t("pick.loadMore", { count: num(remainingCount()) })) +
          "</button></div>";
      }
      if (state.pageError) {
        html += inlineErrorHTML(t("pick.loadFailed"), "", "pager-retry");
      }
      html += disabledGroupHTML();
    }
    els.foot.innerHTML = html;
  }

  function renderGate() {
    els.body.hidden = true;
    els.gate.hidden = false;

    if (state.gate === "denied") {
      els.gate.innerHTML =
        '<div class="empty-block">' +
        '<p class="empty-title" data-i18n="app.adminOnly">' +
        esc(t("app.adminOnly")) +
        "</p>" +
        '<p class="empty-note" data-i18n="app.adminOnlyHint">' +
        esc(t("app.adminOnlyHint")) +
        "</p>" +
        "</div>";
      return;
    }

    els.gate.innerHTML =
      '<div class="empty-block">' +
      '<p class="empty-title" data-i18n="app.error">' +
      esc(t("app.error")) +
      "</p>" +
      '<div class="empty-actions">' +
      '<button type="button" class="btn-ghost" data-role="gate-retry" data-i18n="state.retry">' +
      esc(t("state.retry")) +
      "</button>" +
      "</div>" +
      "</div>";
  }

  function render() {
    if (!els) {
      return;
    }

    if (state.gate === "denied" || state.gate === "error") {
      renderGate();
      return;
    }

    els.gate.hidden = true;
    els.body.hidden = false;

    var searching = state.q.trim() !== "";
    els.clear.hidden = els.search.value === "";

    // A skeleton is only honest when there is nothing on screen yet; a refresh
    // keeps the last rows and says so instead of blanking the list.
    var skeleton = state.gate === "checking" || (state.status === "loading" && state.items.length === 0);
    if (skeleton) {
      els.list.hidden = false;
      els.list.setAttribute("aria-busy", "true");
      els.list.innerHTML = skeletonHTML(6);
      els.foot.hidden = true;
      els.foot.innerHTML = "";
      els.result.hidden = true;
      els.empty.hidden = true;
      els.error.hidden = true;
      els.error.innerHTML = "";
      return;
    }

    els.list.setAttribute("aria-busy", state.status === "loading" ? "true" : "false");
    renderList(searching);
    renderResult(searching);
    renderEmpty(searching);
    renderError();
    renderFoot(searching);
    applyRowTexts();
    syncSelection();
  }

  function renderTexts() {
    if (!els) {
      return;
    }
    if (!els.result.hidden) {
      els.result.textContent = resultText();
    }
    if (!els.empty.hidden) {
      els.emptyTitle.textContent = t("pick.noResults");
    }
    if (state.status === "error") {
      renderError();
    }
    // Only the footer is rebuilt: the list, the search draft and its focus stay.
    renderFoot(state.q.trim() !== "");
    applyRowTexts();
    syncSelection();
  }

  /* Data ------------------------------------------------------------------ */

  function startCheck(force) {
    state.gate = "checking";
    render();
    state.mePromise = core
      .me(force)
      .then(function (me) {
        state.me = me || null;
        if (!me || me.role !== "admin") {
          state.gate = "denied";
          render();
          return;
        }
        state.gate = "ready";
        loadAll();
      })
      .catch(function () {
        state.me = null;
        state.mePromise = null;
        state.gate = "error";
        render();
      });
    return state.mePromise;
  }

  function loadAll() {
    if (state.gate !== "ready") {
      return Promise.resolve();
    }

    state.items = [];
    state.total = 0;
    state.fetched = 0;
    state.nextOffset = null;
    state.loaded = false;
    state.loadedAt = null;
    state.status = "loading";
    state.loadingMore = false;
    state.pageError = false;
    state.pinned = null;
    state.seq += 1;
    state.disabled = freshDisabled();
    render();

    loadPinned();
    probeDisabled();
    return loadActive(true, { clearRows: true });
  }

  function refresh() {
    return loadActive(true, { clearRows: false });
  }

  // A stale remount reloads the counts in place: the old rows stay on screen
  // until the new page lands, so a failure keeps the list and says how old it is.
  function refreshAll() {
    loadPinned();
    probeDisabled();
    return refresh();
  }

  function loadPinned() {
    if (!state.me) {
      return;
    }
    var seq = ++state.pinnedSeq;
    api()
      .adminUsers({ q: String(state.me.userId), limit: 10, sort: "recent" })
      .then(function (payload) {
        if (seq !== state.pinnedSeq) {
          return;
        }
        var rows = normalize(payload);
        var found = null;
        for (var i = 0; i < rows.length; i++) {
          if (String(rows[i].userId) === String(state.me.userId)) {
            found = rows[i];
            break;
          }
        }
        state.pinned = found || itemFromMe(state.me);
        render();
      })
      .catch(function () {
        if (seq !== state.pinnedSeq) {
          return;
        }
        // The row still belongs on top; only its keyword count is unknown.
        if (!state.pinned) {
          state.pinned = itemFromMe(state.me);
        }
        render();
      });
  }

  // A page from the configured endpoint may still contain disabled accounts
  // when the server does not understand the enabled filter. Keep them for the
  // collapsed group and stop trusting the totals, which then count everyone.
  function collectDisabled(rows) {
    var found = rows.filter(function (item) {
      return item.enabled === false;
    });
    if (found.length === 0) {
      return;
    }
    var disabled = state.disabled;
    disabled.mode = "client";
    disabled.total = null;
    for (var i = 0; i < found.length; i++) {
      var exists = false;
      for (var j = 0; j < disabled.items.length; j++) {
        if (String(disabled.items[j].userId) === String(found[i].userId)) {
          exists = true;
          break;
        }
      }
      if (!exists) {
        disabled.items.push(found[i]);
      }
    }
  }

  function loadActive(reset, options) {
    var opts = options || {};
    var searching = state.q.trim() !== "";
    if (!reset && (state.nextOffset === null || state.loadingMore)) {
      return Promise.resolve();
    }
    var offset = reset ? 0 : state.nextOffset;
    var seq = ++state.seq;

    if (opts.clearRows) {
      state.items = [];
      state.total = 0;
      state.fetched = 0;
      state.nextOffset = null;
    }
    state.pageError = false;
    if (reset) {
      state.status = "loading";
    } else {
      state.loadingMore = true;
    }
    render();

    var params = { q: state.q, offset: offset, limit: PAGE_SIZE, sort: "recent" };
    if (!searching) {
      params.enabled = "true";
    }

    return api()
      .adminUsers(params)
      .then(function (payload) {
        if (seq !== state.seq) {
          return;
        }
        var rows = normalize(payload);
        var rawCount = payload && Array.isArray(payload.items) ? payload.items.length : 0;
        var visible = rows;

        if (!searching) {
          var myId = state.me ? String(state.me.userId) : "";
          // The pinned row is rendered separately and must not repeat here.
          visible = rows.filter(function (item) {
            return String(item.userId) !== myId;
          });
          collectDisabled(rows);
        }

        state.items = reset ? visible : state.items.concat(visible);
        state.total = numberOrZero(payload && payload.total);
        state.fetched = reset ? rawCount : state.fetched + rawCount;
        state.nextOffset = nextOffsetOf(payload, rawCount);
        state.status = "ready";
        state.loaded = true;
        state.loadedAt = new Date().toISOString();
        state.loadingMore = false;
        render();
      })
      .catch(function () {
        if (seq !== state.seq) {
          return;
        }
        state.loadingMore = false;
        if (reset) {
          // A failed refresh keeps the rows and the time they came from.
          state.status = "error";
        } else {
          // The cursor only moves on a 2xx page, so a failed page rolls back
          // to the offset it was requested with.
          state.pageError = true;
        }
        render();
      });
  }

  // One tiny request gives the collapsed row its count before it is opened.
  function probeDisabled() {
    var disabled = state.disabled;
    var seq = ++disabled.probeSeq;
    return api()
      .adminUsers({ enabled: "false", limit: DISABLED_PROBE_LIMIT, sort: "recent" })
      .then(function (payload) {
        if (seq !== disabled.probeSeq) {
          return;
        }
        var rows = normalize(payload);
        var honored = rows.every(function (item) {
          return item.enabled === false;
        });
        if (honored) {
          if (disabled.mode !== "server") {
            disabled.mode = "server";
            disabled.total = numberOrZero(payload && payload.total);
          }
        } else {
          degradedDisabledMode(disabled);
        }
        render();
      })
      .catch(function (err) {
        if (seq !== disabled.probeSeq) {
          return;
        }
        if (err && err.status === 400) {
          degradedDisabledMode(disabled);
        }
        render();
      });
  }

  function degradedDisabledMode(disabled) {
    // The endpoint ignored (or rejected) the filter: rows still come from the
    // recent scan, but no count can be trusted.
    disabled.mode = "client";
    disabled.total = null;
  }

  function loadDisabled(reset) {
    var disabled = state.disabled;
    if (disabled.loading) {
      return Promise.resolve();
    }
    if (disabled.mode === "client") {
      return loadDisabledFallback();
    }
    if (!reset && disabled.nextOffset === null) {
      return Promise.resolve();
    }

    var seq = ++disabled.seq;
    var offset = reset ? 0 : disabled.nextOffset;
    if (reset) {
      disabled.items = [];
      disabled.fetched = 0;
      disabled.nextOffset = null;
    }
    disabled.loading = true;
    disabled.error = false;
    render();

    return api()
      .adminUsers({ enabled: "false", offset: offset, limit: PAGE_SIZE, sort: "recent" })
      .then(function (payload) {
        if (seq !== disabled.seq) {
          return;
        }
        var rows = normalize(payload);
        var honored = rows.every(function (item) {
          return item.enabled === false;
        });
        if (!honored) {
          disabled.items = [];
          disabled.fetched = 0;
          disabled.nextOffset = null;
          disabled.loading = false;
          degradedDisabledMode(disabled);
          return loadDisabledFallback();
        }
        disabled.mode = "server";
        disabled.total = numberOrZero(payload && payload.total);
        disabled.items = reset ? rows : disabled.items.concat(rows);
        disabled.fetched = reset ? rows.length : disabled.fetched + rows.length;
        disabled.nextOffset = nextOffsetOf(payload, rows.length);
        disabled.loading = false;
        disabled.loaded = true;
        render();
      })
      .catch(function (err) {
        if (seq !== disabled.seq) {
          return;
        }
        disabled.loading = false;
        if (err && err.status === 400) {
          degradedDisabledMode(disabled);
          return loadDisabledFallback();
        }
        disabled.error = true;
        render();
      });
  }

  // Fallback for an endpoint without the enabled filter: scan the recent page
  // and keep the disabled rows it happens to contain. Counts stay hidden.
  function loadDisabledFallback() {
    var disabled = state.disabled;
    if (disabled.loading) {
      return Promise.resolve();
    }
    if (disabled.items.length > 0) {
      disabled.loaded = true;
      disabled.error = false;
      render();
      return Promise.resolve();
    }

    var seq = ++disabled.seq;
    disabled.loading = true;
    disabled.error = false;
    render();

    return api()
      .adminUsers({ offset: 0, limit: PAGE_SIZE, sort: "recent" })
      .then(function (payload) {
        if (seq !== disabled.seq) {
          return;
        }
        var rows = normalize(payload).filter(function (item) {
          return item.enabled === false;
        });
        disabled.mode = "client";
        disabled.total = null;
        disabled.items = rows;
        disabled.fetched = rows.length;
        disabled.nextOffset = null;
        disabled.loading = false;
        disabled.loaded = true;
        render();
      })
      .catch(function () {
        if (seq !== disabled.seq) {
          return;
        }
        disabled.loading = false;
        disabled.error = true;
        render();
      });
  }

  /* Actions --------------------------------------------------------------- */

  function pick(item) {
    if (!item) {
      return;
    }
    // Library owns the read-only rendering; this page never fetches keywords.
    // Picking yourself is the way back to your own writable list, so it clears
    // the read-only target instead of entering read-only on your own account.
    var isMe = state.me && String(item.userId) === String(state.me.userId);
    core.state.viewingUser = isMe ? null : item;
    core.go("library");
  }

  function normalizeQuery(value) {
    return String(value == null ? "" : value).trim();
  }

  function commitSearch() {
    if (!els || state.gate !== "ready") {
      return;
    }
    var next = normalizeQuery(els.search.value);
    els.clear.hidden = els.search.value === "";
    if (next === state.q) {
      return;
    }

    state.q = next;
    state.items = [];
    state.total = 0;
    state.fetched = 0;
    state.nextOffset = null;
    state.loaded = false;
    state.loadedAt = null;
    state.status = "loading";
    state.loadingMore = false;
    state.pageError = false;
    state.seq += 1;
    render();
    loadActive(true, { clearRows: true });
  }

  function clearSearch() {
    if (!els) {
      return;
    }
    if (els.search.value !== "") {
      els.search.value = "";
    }
    els.clear.hidden = true;
    commitSearch();
    els.search.focus();
  }

  function toggleDisabled() {
    var disabled = state.disabled;
    if (disabled.expanded) {
      disabled.expanded = false;
      render();
      return;
    }
    disabled.expanded = true;
    if (!disabled.loaded && !disabled.loading) {
      loadDisabled(true);
      return;
    }
    render();
  }

  function restoreScroll() {
    if (els && state.scrollTop > 0) {
      els.screen.scrollTop = state.scrollTop;
    }
  }

  /* Wiring ---------------------------------------------------------------- */

  function build(section) {
    section.innerHTML = template();
    els = {
      screen: section.querySelector('[data-role="screen"]'),
      body: section.querySelector('[data-slot="body"]'),
      search: section.querySelector('[data-role="search"]'),
      clear: section.querySelector('[data-role="clear"]'),
      result: section.querySelector('[data-slot="result"]'),
      error: section.querySelector('[data-slot="error"]'),
      list: section.querySelector('[data-slot="list"]'),
      empty: section.querySelector('[data-slot="empty"]'),
      emptyTitle: section.querySelector('[data-slot="empty-title"]'),
      foot: section.querySelector('[data-slot="foot"]'),
      gate: section.querySelector('[data-slot="gate"]'),
    };
  }

  function bind() {
    core.bindBack(sectionEl, function () {
      core.go("library");
    });

    var runSearch = core.debounce(function () {
      commitSearch();
    }, SEARCH_DEBOUNCE_MS);

    els.search.addEventListener("input", function () {
      els.clear.hidden = els.search.value === "";
      if (state.gate !== "ready") {
        return;
      }
      runSearch();
    });

    els.search.addEventListener("keydown", function (event) {
      if (event.key === "Enter") {
        event.preventDefault();
        commitSearch();
      }
    });

    els.screen.addEventListener("scroll", function () {
      state.scrollTop = els.screen.scrollTop;
    });

    sectionEl.addEventListener("click", function (event) {
      var target = event.target;
      if (!target || typeof target.closest !== "function") {
        return;
      }

      var row = target.closest("button.pick-row[data-user-id]");
      if (row) {
        pick(findUser(row.getAttribute("data-user-id")));
        return;
      }
      if (target.closest('[data-role="clear"]') || target.closest('[data-role="clear-search"]')) {
        clearSearch();
        return;
      }
      if (target.closest('[data-role="disabled-toggle"]')) {
        toggleDisabled();
        return;
      }
      if (target.closest('[data-role="load-more"]')) {
        loadActive(false);
        return;
      }
      if (target.closest('[data-role="pager-retry"]')) {
        loadActive(false);
        return;
      }
      if (target.closest('[data-role="disabled-load-more"]')) {
        loadDisabled(false);
        return;
      }
      if (target.closest('[data-role="retry"]')) {
        refresh();
        return;
      }
      if (target.closest('[data-role="disabled-retry"]')) {
        loadDisabled(true);
        return;
      }
      if (target.closest('[data-role="gate-retry"]')) {
        startCheck(true);
      }
    });
  }

  function mount() {
    var section = document.getElementById(SECTION_ID);
    if (!section) {
      return;
    }
    sectionEl = section;
    if (!els) {
      build(section);
      bind();
    }
    if (i18n) {
      i18n.apply(section);
    }

    if (state.gate === "denied" || state.gate === "error") {
      render();
      return;
    }
    if (state.gate === "checking" || !state.me) {
      // A second mount while /api/me is still in flight must not fetch again.
      if (!state.mePromise) {
        startCheck(false);
      }
      return;
    }
    if (!state.loaded) {
      loadAll();
      return;
    }
    if (core.state.isStale(VIEW)) {
      refreshAll();
      return;
    }

    render();
    restoreScroll();
  }

  function onLang() {
    if (!sectionEl) {
      return;
    }
    if (i18n) {
      i18n.apply(sectionEl);
    }
    if (state.gate === "ready") {
      renderTexts();
    }
  }

  core.registerView(VIEW, { mount: mount, onLang: onLang });
})(window);
