/*
 * settings.js — the admin-only Settings view.
 *
 * Four cards share the page: access control (the restricted/open switch),
 * people (who may use the bot), a read-only deployment status, and the recently
 * denied accounts with a one-tap "Allow" action.
 *
 * Permission flow, in order:
 *   1. GET /api/me decides whether the Settings tab exists for this account.
 *      The nav entry ships `hidden` in index.html, so a non-admin never sees it.
 *   2. Opening #/settings without the admin role renders an "Admins only" empty
 *      state and issues no /api/admin request at all.
 *   3. Any request answered 403 "forbidden" means the account was revoked while
 *      the app was open: the whole view is replaced by a revocation notice and
 *      every later request short-circuits.
 *
 * Transport: window.MagnetAPI has no admin wrappers on purpose (they are not
 * part of the member surface), so this file calls MagnetApp.request directly.
 *
 * Routing note: the shell's route table (js/core.js) lists three views and
 * rejects unknown ones. core.js is out of scope for this change, so when
 * registerView("settings", ...) is refused this module installs a small
 * activation adapter: it registers its hashchange listener before core binds
 * its own, claims "#/settings", and lets every other route through untouched.
 * If the shell ever learns the route, registration succeeds and the adapter
 * steps aside automatically.
 */
(function (global) {
  "use strict";

  var core = global.MagnetApp;
  if (!core) {
    return;
  }

  var VIEW = "settings";
  var SECTION_ID = "view-settings";
  var ROUTE_HASH = "#/settings";
  var VIEW_TITLE = "Settings";

  var PEOPLE_PAGE_SIZE = 20;
  var SEARCH_DEBOUNCE_MS = 300;

  var MODE_OFF = "off";
  var MODE_RESTRICTED = "restricted";

  // Sampled before core.init() has a chance to normalise an unknown deep link
  // to #/today; only the adapter needs it.
  var initialHash = (global.location && global.location.hash) || "";

  var els = null;
  var coreRoutesSettings = false;
  var adapterInstalled = false;

  var me = { status: "idle", role: "", accessMode: "", loaded: false, promise: null };
  var access = { mode: "", saving: false };
  var people = {
    q: "",
    items: [],
    total: 0,
    nextOffset: null,
    status: "idle",
    loaded: false,
    error: null,
    seq: 0,
  };
  var status = { data: null, status: "idle", error: null };
  var denied = [];
  // Ids this session already granted through the denied list. The status
  // snapshot can keep listing them for a while (it is a server-side ring
  // buffer), so they are filtered out locally instead of reappearing.
  var allowedIds = Object.create(null);
  var revoked = false;

  var dialog = {
    el: null,
    title: null,
    body: null,
    note: null,
    confirm: null,
    cancel: null,
    close: null,
    onConfirm: null,
    confirmLabel: "Save",
    pendingLabel: "Saving\u2026",
    pending: false,
    locked: false,
    lastFocus: null,
  };

  /* Transport ------------------------------------------------------------ */

  function request(method, path, body) {
    var options = { method: method };
    if (body !== undefined) {
      options.body = body;
    }
    return core.request(path, options);
  }

  function withQuery(path, params) {
    var search = new URLSearchParams();
    Object.keys(params || {}).forEach(function (key) {
      var value = params[key];
      if (value === undefined || value === null || value === "") {
        return;
      }
      search.set(key, String(value));
    });
    var encoded = search.toString();
    return encoded ? path + "?" + encoded : path;
  }

  /* Helpers -------------------------------------------------------------- */

  function esc(value) {
    return core.escapeHTML(value);
  }

  function pad2(value) {
    return value < 10 ? "0" + value : String(value);
  }

  // The API sends RFC3339 UTC instants; the date shown is the viewer's local
  // date, and a plain YYYY-MM-DD value is passed through untouched so it cannot
  // shift a day in a negative offset.
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

  function numberOrZero(value) {
    var number = Number(value);
    return isFinite(number) ? number : 0;
  }

  function modeLabel(mode) {
    return mode === MODE_RESTRICTED ? "Restricted" : "Open";
  }

  function modeHint(mode) {
    return mode === MODE_RESTRICTED
      ? "Only people listed below can use the bot."
      : "Anyone who finds the bot can use it.";
  }

  function normalize(value) {
    return String(value == null ? "" : value).trim();
  }

  // "#/settings" -> "settings"; the hash parser mirrors the shell's own
  // normalisation so both agree on what a route name is.
  function routeName(hash) {
    return String(hash == null ? "" : hash)
      .replace(/^#\/?/, "")
      .replace(/\/+$/, "");
  }

  /* Permission and fatal states ----------------------------------------- */

  function navLink() {
    return document.querySelector('#tabbar a[data-route="#/settings"]');
  }

  function showNav() {
    var link = navLink();
    if (link) {
      link.hidden = false;
    }
  }

  function hideNav() {
    var link = navLink();
    if (link) {
      link.hidden = true;
    }
  }

  function isRevoked(err) {
    return Boolean(err) && err.status === 403 && err.code === "forbidden";
  }

  function isNotAdmin(err) {
    return Boolean(err) && err.status === 403 && err.code === "forbidden_admin";
  }

  // Turns a rejected request into the state it implies. Returns true when the
  // error was consumed and the caller must not render a local error.
  function handleFatal(err) {
    if (isRevoked(err)) {
      showRevoked();
      return true;
    }
    if (isNotAdmin(err)) {
      me.role = "user";
      hideNav();
      showAdminsOnly();
      return true;
    }
    return false;
  }

  function errorMessage(err, fallback) {
    if (err && err.status === 429) {
      return "Too many requests, try again in a minute";
    }
    if (err && err.status === 409 && err.message) {
      return err.message;
    }
    var message = err && err.message ? String(err.message) : "";
    // core.js falls back to "Request failed (<status>)" when the server sends
    // no message; that is a transport detail, not something to show a person.
    if (!message || /^Request failed \(\d+\)$/.test(message)) {
      return fallback || "Something went wrong. Please try again.";
    }
    return message;
  }

  function toastError(err, fallback) {
    core.toast(errorMessage(err, fallback), "error");
  }

  /* Access control card -------------------------------------------------- */

  function renderAccess() {
    if (!els) {
      return;
    }

    var restricted = access.mode === MODE_RESTRICTED;
    els.accessMode.textContent = modeLabel(access.mode);
    els.accessHint.textContent = modeHint(access.mode);
    els.accessToggle.checked = restricted;
    els.accessToggle.disabled = access.saving;
  }

  function applyAccessMode(mode) {
    access.mode = mode === MODE_RESTRICTED ? MODE_RESTRICTED : MODE_OFF;
    if (status.data) {
      status.data.accessMode = access.mode;
      renderStatus();
    }
    renderAccess();
  }

  function patchAccessMode(mode) {
    if (access.saving) {
      return Promise.resolve();
    }

    access.saving = true;
    renderAccess();

    return request("PATCH", "/api/admin/settings", { accessMode: mode })
      .then(function (payload) {
        access.saving = false;
        applyAccessMode(payload && payload.accessMode ? payload.accessMode : mode);
        core.toast(
          access.mode === MODE_RESTRICTED ? "Access is now restricted" : "Access is now open",
          "success"
        );
      })
      .catch(function (err) {
        // The checkbox is restored by renderAccess() from the unchanged mode.
        access.saving = false;
        renderAccess();
        if (handleFatal(err)) {
          return;
        }
        toastError(err, "Could not update access mode.");
      });
  }

  function openOpenAccessDialog() {
    var message = document.createElement("p");
    message.className = "dialog-message";
    message.textContent = "Anyone will be able to use this bot.";

    openDialog({
      title: "Turn off access control?",
      content: message,
      confirmLabel: "Turn off access control",
      pendingLabel: "Turning off\u2026",
      danger: true,
      onConfirm: function () {
        return patchAccessMode(MODE_OFF).then(function () {
          closeDialog();
        });
      },
    });
  }

  function onAccessToggle() {
    if (!els || access.saving) {
      return;
    }

    if (els.accessToggle.checked) {
      patchAccessMode(MODE_RESTRICTED);
      return;
    }

    // Opening the bot to everyone is confirmed first; until then the switch
    // shows the mode that is actually in effect.
    els.accessToggle.checked = true;
    openOpenAccessDialog();
  }

  /* People --------------------------------------------------------------- */

  function personRow(item) {
    var id = esc(String(item.userId));
    var remark = normalize(item.remark);
    var badge =
      item.role === "admin"
        ? '<span class="badge badge-admin">admin</span>'
        : '<span class="badge badge-user">user</span>';
    var state = item.enabled
      ? '<span class="person-state is-active">active</span>'
      : '<span class="person-state is-disabled">disabled</span>';
    var seen = item.lastSeen ? esc(formatDate(item.lastSeen)) : "\u2014";
    var lock = item.fromEnv
      ? '<span class="person-lock" title="Managed by environment variables"' +
        ' aria-label="Managed by environment variables">\ud83d\udd12</span>'
      : "";

    return (
      '<li class="person-row" data-user-id="' +
      id +
      '">' +
      '<span class="person-id">#' +
      id +
      "</span>" +
      '<span class="person-main">' +
      '<span class="person-remark" title="' +
      esc(remark) +
      '">' +
      (remark ? esc(remark) : "\u2014") +
      "</span>" +
      '<span class="person-meta">' +
      badge +
      state +
      '<span class="person-seen" title="Last seen">' +
      seen +
      "</span>" +
      lock +
      "</span>" +
      "</span>" +
      '<span class="person-actions">' +
      '<button type="button" class="icon-btn" data-action="edit" data-user-id="' +
      id +
      '" title="Edit access" aria-label="Edit access for #' +
      id +
      '">\u270f\ufe0f</button>' +
      '<button type="button" class="icon-btn" data-action="revoke" data-user-id="' +
      id +
      '" title="Revoke access" aria-label="Revoke access for #' +
      id +
      '">\ud83d\uddd1</button>' +
      "</span>" +
      "</li>"
    );
  }

  function renderPeople() {
    if (!els) {
      return;
    }

    var showSkeleton = people.status === "loading" && people.items.length === 0;
    var showEmpty = people.status === "ready" && people.items.length === 0;
    var showList = people.items.length > 0;

    els.peopleLoading.hidden = !showSkeleton;
    els.peopleError.hidden = people.status !== "error";
    els.peopleEmpty.hidden = !showEmpty;
    els.peopleList.hidden = !showList;

    var hasCursor = people.nextOffset !== null;
    els.peopleMore.hidden = !hasCursor || !showList;
    els.peopleMore.disabled = people.status === "loading" || people.status === "error";

    if (people.status === "error") {
      renderRetry(els.peopleError, "Could not load people.", function () {
        loadPeople(true);
      });
    }

    if (showEmpty) {
      els.peopleEmpty.textContent = people.q
        ? 'No one matches "' + people.q + '".'
        : "No accounts yet. Use + Add to allow someone.";
    }

    if (showList) {
      els.peopleList.innerHTML = people.items.map(personRow).join("");
    } else {
      els.peopleList.innerHTML = "";
    }
  }

  function findPerson(id) {
    var wanted = String(id);
    for (var i = 0; i < people.items.length; i++) {
      if (String(people.items[i].userId) === wanted) {
        return people.items[i];
      }
    }
    return null;
  }

  function normalizePeople(payload) {
    var raw = payload && Array.isArray(payload.items) ? payload.items : [];
    var result = [];
    for (var i = 0; i < raw.length; i++) {
      var item = raw[i];
      if (!item || typeof item !== "object") {
        continue;
      }
      result.push({
        userId: item.userId,
        remark: typeof item.remark === "string" ? item.remark : "",
        role: item.role === "admin" ? "admin" : "user",
        enabled: item.enabled === true,
        fromEnv: item.fromEnv === true,
        createdAt: item.createdAt,
        lastSeen: item.lastSeen || null,
      });
    }
    return result;
  }

  function loadPeople(reset) {
    if (revoked) {
      return Promise.resolve();
    }
    if (!reset && people.nextOffset === null) {
      return Promise.resolve();
    }

    var offset = reset ? 0 : people.nextOffset;
    var seq = ++people.seq;

    if (reset) {
      people.items = [];
      people.nextOffset = null;
    }
    people.status = "loading";
    people.error = null;
    renderPeople();

    return request("GET", withQuery("/api/admin/users", { q: people.q, offset: offset, limit: PEOPLE_PAGE_SIZE }))
      .then(function (payload) {
        if (seq !== people.seq) {
          return;
        }
        var page = normalizePeople(payload);
        var rawItems = payload && Array.isArray(payload.items) ? payload.items.length : 0;
        people.items = reset ? page : people.items.concat(page);
        people.total = numberOrZero(payload && payload.total);
        people.nextOffset =
          payload && typeof payload.nextOffset === "number" && isFinite(payload.nextOffset)
            ? payload.nextOffset
            : null;
        people.status = "ready";
        people.loaded = true;
        renderPeople();

        // An empty page with a cursor still advertised would page forever.
        if (rawItems === 0) {
          people.nextOffset = null;
          renderPeople();
        }
      })
      .catch(function (err) {
        if (seq !== people.seq) {
          return;
        }
        people.status = "error";
        people.error = err;
        renderPeople();
        handleFatal(err);
      });
  }

  /* People dialogs ------------------------------------------------------- */

  function selectElement(options, value) {
    var select = document.createElement("select");
    select.className = "dialog-select";
    options.forEach(function (spec) {
      var option = document.createElement("option");
      option.value = spec.value;
      option.textContent = spec.label;
      select.appendChild(option);
    });
    select.value = value;
    return select;
  }

  function field(labelText, control) {
    var wrapper = document.createElement("label");
    wrapper.className = "dialog-field";

    var caption = document.createElement("span");
    caption.className = "dialog-label";
    caption.textContent = labelText;

    wrapper.appendChild(caption);
    wrapper.appendChild(control);
    return wrapper;
  }

  function textInput(value, placeholder, ariaLabel) {
    var input = document.createElement("input");
    input.type = "text";
    input.className = "dialog-input";
    input.value = value || "";
    if (placeholder) {
      input.placeholder = placeholder;
    }
    if (ariaLabel) {
      input.setAttribute("aria-label", ariaLabel);
    }
    return input;
  }

  function openAddDialog() {
    var textarea = document.createElement("textarea");
    textarea.className = "dialog-textarea";
    textarea.rows = 3;
    textarea.placeholder = "Telegram user IDs, separated by commas, spaces or newlines";
    textarea.setAttribute("aria-label", "Telegram user IDs");
    textarea.value = "";
    textarea.addEventListener("input", refreshConfirm);

    var role = selectElement(
      [
        { value: "user", label: "user" },
        { value: "admin", label: "admin" },
      ],
      "user"
    );
    var remark = textInput("", "Remark (optional)", "Remark");

    var body = document.createElement("div");
    body.className = "dialog-stack";
    body.appendChild(field("User IDs", textarea));
    body.appendChild(field("Role", role));
    body.appendChild(field("Remark", remark));

    openDialog({
      title: "Add people",
      content: body,
      field: textarea,
      confirmLabel: "Add",
      pendingLabel: "Adding\u2026",
      focus: textarea,
      onConfirm: function () {
        var ids = normalizeIDs(textarea.value);
        if (!ids) {
          return;
        }

        return request("POST", "/api/admin/users", {
          userIds: ids,
          role: role.value,
          remark: normalize(remark.value),
        })
          .then(function (payload) {
            var added = payload && Array.isArray(payload.added) ? payload.added.length : 0;
            var updated = payload && Array.isArray(payload.updated) ? payload.updated.length : 0;
            var ignored = payload && Array.isArray(payload.ignored) ? payload.ignored.length : 0;
            var message = "Added " + added + ", updated " + updated;
            if (ignored > 0) {
              message += ", ignored " + ignored;
            }
            core.toast(message, "success");
            closeDialog();
            refreshAfterWrite();
          })
          .catch(function (err) {
            if (handleFatal(err)) {
              return;
            }
            if (err && err.status === 409) {
              showDialogNote(errorMessage(err, "This change is not allowed."));
              return;
            }
            toastError(err, "Could not add those people.");
          });
      },
    });
  }

  // The server splits the batch on commas only, so spaces and newlines from a
  // pasted list are normalised here instead of arriving as one unparsable id.
  function normalizeIDs(raw) {
    var tokens = String(raw || "")
      .split(/[\s,]+/)
      .map(function (token) {
        return token.trim();
      })
      .filter(Boolean);
    return tokens.join(",");
  }

  function openEditDialog(item) {
    var locked = item.fromEnv === true;

    var remark = textInput(item.remark || "", "Remark", "Remark");
    var role = selectElement(
      [
        { value: "user", label: "user" },
        { value: "admin", label: "admin" },
      ],
      item.role === "admin" ? "admin" : "user"
    );

    var enabledWrap = document.createElement("label");
    enabledWrap.className = "dialog-check";
    var enabled = document.createElement("input");
    enabled.type = "checkbox";
    enabled.checked = item.enabled === true;
    enabled.setAttribute("aria-label", "Access enabled");
    var enabledText = document.createElement("span");
    enabledText.textContent = "Access enabled";
    enabledWrap.appendChild(enabled);
    enabledWrap.appendChild(enabledText);

    remark.disabled = locked;
    role.disabled = locked;
    enabled.disabled = locked;

    var body = document.createElement("div");
    body.className = "dialog-stack";
    body.appendChild(field("Remark", remark));
    body.appendChild(field("Role", role));
    body.appendChild(enabledWrap);

    if (locked) {
      var note = document.createElement("p");
      note.className = "dialog-hint";
      note.textContent = "Managed by environment variables";
      body.appendChild(note);
    }

    openDialog({
      title: "Edit #" + item.userId,
      content: body,
      confirmLabel: "Save",
      pendingLabel: "Saving\u2026",
      focus: remark,
      confirmDisabled: locked,
      onConfirm: function () {
        if (locked) {
          return;
        }

        return request("PATCH", "/api/admin/users/" + encodeURIComponent(String(item.userId)), {
          remark: normalize(remark.value),
          role: role.value,
          enabled: enabled.checked,
        })
          .then(function (payload) {
            applyPersonUpdate(item.userId, payload);
            core.toast("Saved", "success");
            closeDialog();
            refreshAfterWrite();
          })
          .catch(function (err) {
            if (handleFatal(err)) {
              return;
            }
            if (err && err.status === 409) {
              showDialogNote(errorMessage(err, "This change is not allowed."));
              return;
            }
            toastError(err, "Could not save that change.");
          });
      },
    });
  }

  function applyPersonUpdate(userID, payload) {
    var updated = payload && typeof payload === "object" ? normalizePeople({ items: [payload] })[0] : null;
    if (!updated) {
      return;
    }
    for (var i = 0; i < people.items.length; i++) {
      if (String(people.items[i].userId) === String(userID)) {
        people.items[i] = updated;
        break;
      }
    }
    renderPeople();
  }

  function openRevokeDialog(item) {
    var message = document.createElement("p");
    message.className = "dialog-message";
    message.textContent = "Access is revoked. Their keywords, history and alarm records are kept.";

    openDialog({
      title: "Revoke access for #" + item.userId + "?",
      content: message,
      confirmLabel: "Revoke access",
      pendingLabel: "Revoking\u2026",
      danger: true,
      focus: dialog.cancel,
      onConfirm: function () {
        return request("DELETE", "/api/admin/users/" + encodeURIComponent(String(item.userId)))
          .then(function () {
            removePerson(item.userId);
            core.toast("Access revoked", "success");
            closeDialog();
            refreshAfterWrite();
          })
          .catch(function (err) {
            if (handleFatal(err)) {
              return;
            }
            if (err && err.status === 409) {
              showDialogNote(errorMessage(err, "This change is not allowed."));
              return;
            }
            toastError(err, "Could not revoke that access.");
          });
      },
    });
  }

  function removePerson(userID) {
    for (var i = 0; i < people.items.length; i++) {
      if (String(people.items[i].userId) === String(userID)) {
        people.items.splice(i, 1);
        break;
      }
    }
    renderPeople();
  }

  /* Status card ---------------------------------------------------------- */

  function statusRow(label, valueHTML) {
    return (
      '<div class="status-row">' +
      '<span class="status-key">' +
      esc(label) +
      "</span>" +
      '<span class="status-value">' +
      valueHTML +
      "</span>" +
      "</div>"
    );
  }

  function renderStatus() {
    if (!els) {
      return;
    }

    var data = status.data;
    if (!data) {
      els.statusGrid.innerHTML =
        '<div class="status-row"><span class="status-key">Status</span>' +
        '<span class="status-value">' +
        esc(status.status === "error" ? "Could not load status." : "Loading\u2026") +
        "</span></div>";
      return;
    }

    var counts = data.counts || {};
    var rows = [];

    rows.push(statusRow("Version", esc(data.version || "\u2014")));
    rows.push(statusRow("Build", esc(data.buildTime || "\u2014")));
    rows.push(statusRow("Access mode", esc(modeLabel(data.accessMode))));

    var countRows = [
      ["Users", counts.users],
      ["Admins", counts.admins],
      ["Keywords", counts.keywords],
      ["Histories", counts.histories],
      ["Alarm records", counts.alarms],
    ];
    for (var i = 0; i < countRows.length; i++) {
      rows.push(statusRow(countRows[i][0], esc(String(numberOrZero(countRows[i][1])))));
    }

    if (!data.webhook) {
      rows.push(statusRow("Webhook", "Webhook not configured (polling mode)"));
    } else {
      rows.push(statusRow("Webhook URL", esc(data.webhook.url || "\u2014")));
      rows.push(statusRow("Last error", esc(data.webhook.lastError || "\u2014")));
      rows.push(statusRow("Pending", esc(String(numberOrZero(data.webhook.pending)))));
    }

    els.statusGrid.innerHTML = rows.join("");
  }

  function loadStatus() {
    if (revoked) {
      return Promise.resolve();
    }

    status.status = "loading";
    renderStatus();

    return request("GET", "/api/admin/status")
      .then(function (payload) {
        status.data = payload && typeof payload === "object" ? payload : null;
        status.status = "ready";
        if (status.data) {
          if (access.mode === "") {
            applyAccessMode(status.data.accessMode);
          } else {
            renderAccess();
          }
          denied = (Array.isArray(status.data.recentDenied) ? status.data.recentDenied : []).filter(
            function (item) {
              return !allowedIds[String(item.userId)];
            }
          );
        }
        renderStatus();
        renderDenied();
      })
      .catch(function (err) {
        status.status = "error";
        status.error = err;
        renderStatus();
        handleFatal(err);
      });
  }

  /* Recent denied -------------------------------------------------------- */

  function deniedName(item) {
    var name = normalize(item.name);
    var username = normalize(item.username);
    if (name) {
      return name;
    }
    if (username) {
      return "@" + username;
    }
    return "\u2014";
  }

  function renderDenied() {
    if (!els) {
      return;
    }

    if (!denied.length) {
      els.deniedCard.hidden = true;
      els.deniedList.innerHTML = "";
      return;
    }

    els.deniedCard.hidden = false;
    els.deniedList.innerHTML = denied
      .map(function (item) {
        var id = esc(String(item.userId));
        return (
          '<li class="denied-row">' +
          '<span class="denied-line">#' +
          id +
          " \u00b7 " +
          esc(deniedName(item)) +
          " \u00b7 " +
          esc(formatDate(item.at)) +
          "</span>" +
          '<button type="button" class="btn-ghost denied-allow" data-action="allow" data-user-id="' +
          id +
          '" aria-label="Allow #' +
          id +
          '">Allow</button>' +
          "</li>"
        );
      })
      .join("");
  }

  function allowDenied(userID) {
    if (revoked) {
      return;
    }

    request("POST", "/api/admin/users", { userIds: String(userID), role: "user", remark: "" })
      .then(function (payload) {
        var added = payload && Array.isArray(payload.added) ? payload.added.length : 0;
        core.toast(added > 0 ? "Access granted" : "Access updated", "success");
        allowedIds[String(userID)] = true;
        denied = denied.filter(function (item) {
          return String(item.userId) !== String(userID);
        });
        renderDenied();
        loadPeople(true);
        loadStatus();
      })
      .catch(function (err) {
        if (handleFatal(err)) {
          return;
        }
        toastError(err, "Could not grant access.");
      });
  }

  /* Loading -------------------------------------------------------------- */

  function loadAccess() {
    if (revoked) {
      return Promise.resolve();
    }

    return request("GET", "/api/admin/settings")
      .then(function (payload) {
        applyAccessMode(payload && payload.accessMode ? payload.accessMode : MODE_OFF);
      })
      .catch(function (err) {
        if (handleFatal(err)) {
          return;
        }
        renderAccess();
      });
  }

  function loadAll() {
    if (revoked) {
      return;
    }
    loadAccess();
    loadPeople(true);
    loadStatus();
  }

  function refreshAfterWrite() {
    if (revoked) {
      return;
    }
    loadPeople(true);
    loadStatus();
  }

  /* Section states ------------------------------------------------------- */

  function showLoading() {
    els.loading.hidden = false;
    els.content.hidden = true;
    els.empty.hidden = true;
    els.error.hidden = true;
  }

  function showContent() {
    els.loading.hidden = true;
    els.content.hidden = false;
    els.empty.hidden = true;
    els.error.hidden = true;
    renderAccess();
  }

  function showAdminsOnly() {
    if (!els) {
      return;
    }
    els.loading.hidden = true;
    els.content.hidden = true;
    els.error.hidden = true;
    els.empty.hidden = false;
    els.empty.textContent = "";
    var title = document.createElement("strong");
    title.className = "empty-title";
    title.textContent = "Admins only";
    var hint = document.createElement("span");
    hint.className = "empty-hint";
    hint.textContent = "This page is available to administrators. Ask the administrator if you need access.";
    els.empty.appendChild(title);
    els.empty.appendChild(hint);
  }

  function ensureDom() {
    var section = document.getElementById(SECTION_ID);
    if (!section) {
      return false;
    }
    if (!els) {
      els = build(section);
      bind();
    }
    return true;
  }

  function showRevoked() {
    if (revoked) {
      return;
    }
    // The account may be revoked while it is on another tab, before this view
    // ever mounted; build it here so the notice can replace the page.
    if (!ensureDom()) {
      return;
    }

    revoked = true;
    hideNav();

    if (global.document && document.body) {
      document.body.classList.add("is-access-revoked");
    }

    // The notice owns the viewport: every other section goes away, including
    // one the shell rendered for the route that was active a moment ago.
    var sections = document.querySelectorAll("#view .view");
    for (var i = 0; i < sections.length; i++) {
      sections[i].hidden = sections[i].id !== SECTION_ID;
    }

    els.loading.hidden = true;
    els.content.hidden = true;
    els.empty.hidden = true;
    els.error.hidden = true;

    els.revoked.hidden = false;
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

  function showProfileError() {
    els.loading.hidden = true;
    els.content.hidden = true;
    els.empty.hidden = true;
    els.error.hidden = false;
    renderRetry(els.error, "Could not load your profile.", function () {
      me.loaded = false;
      me.status = "idle";
      loadView();
    });
  }

  /* Me ------------------------------------------------------------------- */

  function ensureMe(force) {
    if (!force && me.loaded) {
      return Promise.resolve(me);
    }
    if (!force && me.promise) {
      return me.promise;
    }
    if (!core.state || !core.state.initData) {
      // core.js shows its "open from the bot" gate; without initData every
      // request would be rejected, so nothing is attempted here.
      return Promise.resolve(null);
    }

    me.promise = request("GET", "/api/me")
      .then(function (payload) {
        me.loaded = true;
        me.status = "ready";
        me.role = payload && payload.role === "admin" ? "admin" : "user";
        me.accessMode = payload && typeof payload.accessMode === "string" ? payload.accessMode : "";
        if (me.role === "admin") {
          showNav();
          if (access.mode === "" && me.accessMode) {
            access.mode = me.accessMode;
          }
        } else {
          hideNav();
        }
        return me;
      })
      .catch(function (err) {
        me.status = "error";
        me.loaded = false;
        if (!handleFatal(err)) {
          hideNav();
        }
        return me;
      })
      .then(function (value) {
        me.promise = null;
        return value;
      });

    return me.promise;
  }

  /* View lifecycle ------------------------------------------------------- */

  function loadView() {
    if (revoked) {
      return;
    }

    showLoading();

    ensureMe().then(function (result) {
      if (revoked) {
        return;
      }
      if (!result || result.status === "error") {
        if (result && result.status === "error" && me.role !== "admin") {
          showProfileError();
        }
        return;
      }
      if (result.role !== "admin") {
        showAdminsOnly();
        return;
      }

      showContent();
      loadAll();
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

    loadView();
  }

  /* Routing adapter (only when the shell does not know this route) --------- */

  function activate() {
    showOnlySettings();
    mount();
  }

  function showOnlySettings() {
    var sections = document.querySelectorAll("#view .view");
    for (var i = 0; i < sections.length; i++) {
      sections[i].hidden = sections[i].id !== SECTION_ID;
    }

    var tabs = document.querySelectorAll("#tabbar [data-route]");
    for (var j = 0; j < tabs.length; j++) {
      var isSettings = routeName(tabs[j].getAttribute("data-route")) === VIEW;
      tabs[j].classList.toggle("is-active", isSettings);
      if (isSettings) {
        tabs[j].setAttribute("aria-current", "page");
      } else {
        tabs[j].removeAttribute("aria-current");
      }
    }

    var title = document.getElementById("app-title");
    if (title) {
      title.textContent = VIEW_TITLE;
    }
  }

  function hideSettingsView() {
    if (coreRoutesSettings) {
      return;
    }

    var section = document.getElementById(SECTION_ID);
    if (section) {
      section.hidden = true;
    }

    var link = navLink();
    if (link) {
      link.classList.remove("is-active");
      link.removeAttribute("aria-current");
    }
  }

  function onHashChange(event) {
    if (coreRoutesSettings) {
      return;
    }

    if (revoked) {
      if (event && typeof event.stopImmediatePropagation === "function") {
        event.stopImmediatePropagation();
      }
      showOnlySettings();
      return;
    }

    if (routeName(global.location.hash) !== VIEW) {
      // Leaving the page: let the shell render the next route, but take the
      // settings section down first.
      hideSettingsView();
      return;
    }

    // Registered before core binds its own handler, so this is the first
    // listener to see #/settings; stopping here keeps core from rewriting an
    // unknown route back to #/today.
    if (event && typeof event.stopImmediatePropagation === "function") {
      event.stopImmediatePropagation();
    }
    activate();
  }

  // The shell reacts to a tab click by rendering the next route before the
  // hash actually changes (an active tab calls route() directly). Taking this
  // section down on the click keeps the two views from overlapping for a frame.
  function onDocumentClick(event) {
    if (coreRoutesSettings || revoked) {
      return;
    }
    var target = event.target;
    var link = target && typeof target.closest === "function" ? target.closest("a[data-route]") : null;
    if (!link || !link.parentNode || link.parentNode.id !== "tabbar") {
      return;
    }
    if (routeName(link.getAttribute("data-route")) !== VIEW) {
      hideSettingsView();
    }
  }

  function installAdapter() {
    if (adapterInstalled) {
      return;
    }
    adapterInstalled = true;
    global.addEventListener("hashchange", onHashChange);
    document.addEventListener("click", onDocumentClick, true);
  }

  function init() {
    if (!core.state || !core.state.initData) {
      return;
    }

    if (!coreRoutesSettings) {
      // A deep link to #/settings was already normalised to #/today by the
      // shell before this handler ran; restore it and draw the page.
      if (routeName(initialHash) === VIEW) {
        try {
          global.history.replaceState(null, "", ROUTE_HASH);
        } catch (err) {
          // Some embedded webviews refuse history writes; the view still works.
        }
        activate();
      }
    }

    ensureMe();
  }

  /* DOM ------------------------------------------------------------------ */

  function build(section) {
    section.innerHTML =
      '<div class="skeleton" data-slot="loading">Loading\u2026</div>' +
      '<div class="error" data-slot="error" role="alert" hidden></div>' +
      '<div class="empty settings-empty" data-slot="empty" hidden></div>' +
      '<div class="settings-revoked" data-slot="revoked" hidden>' +
      '<h2 class="revoked-title">Access revoked. Ask the administrator.</h2>' +
      "</div>" +
      '<div class="settings-content" data-slot="content" hidden>' +
      '<section class="settings-card" data-card="access">' +
      '<h2 class="settings-card-title">Access control</h2>' +
      '<p class="access-mode" data-slot="access-mode"></p>' +
      '<label class="switch-row">' +
      '<input type="checkbox" class="switch-input" data-role="access-toggle" aria-label="Restricted access" />' +
      '<span class="switch-text">Restricted access</span>' +
      "</label>" +
      '<p class="settings-hint" data-slot="access-hint"></p>' +
      "</section>" +
      '<section class="settings-card" data-card="people">' +
      '<div class="settings-card-head">' +
      '<h2 class="settings-card-title">People</h2>' +
      '<button type="button" class="btn settings-add" data-role="add-person">+ Add</button>' +
      "</div>" +
      '<input class="settings-search" type="search" data-role="search" placeholder="Search by id or remark" ' +
      'aria-label="Search people" autocomplete="off" />' +
      '<div class="skeleton" data-slot="people-loading">Loading\u2026</div>' +
      '<div class="error" data-slot="people-error" role="alert" hidden></div>' +
      '<div class="empty" data-slot="people-empty" hidden></div>' +
      '<ul class="people-list" data-slot="people-list" hidden></ul>' +
      '<button type="button" class="btn-ghost load-more" data-role="people-more" hidden>Load more</button>' +
      "</section>" +
      '<section class="settings-card" data-card="status">' +
      '<h2 class="settings-card-title">Status</h2>' +
      '<div class="status-grid" data-slot="status-grid"></div>' +
      "</section>" +
      '<section class="settings-card" data-card="denied" hidden>' +
      '<h2 class="settings-card-title">Recent denied</h2>' +
      '<ul class="denied-list" data-slot="denied-list"></ul>' +
      "</section>" +
      "</div>" +
      '<div class="modal-backdrop settings-dialog" data-role="dialog" hidden>' +
      '<div class="modal" role="dialog" aria-modal="true" aria-labelledby="settings-dialog-title">' +
      '<div class="modal-head">' +
      '<h2 class="modal-title" id="settings-dialog-title" data-slot="dialog-title"></h2>' +
      '<button class="modal-close" type="button" data-role="dialog-close" aria-label="Close dialog">\u2715</button>' +
      "</div>" +
      '<div class="dialog-body" data-slot="dialog-body"></div>' +
      '<p class="dialog-note" data-slot="dialog-note" role="alert" hidden></p>' +
      '<div class="dialog-actions">' +
      '<button class="btn-ghost" type="button" data-role="dialog-cancel">Cancel</button>' +
      '<button class="btn" type="button" data-role="dialog-confirm">Save</button>' +
      "</div>" +
      "</div>" +
      "</div>";

    var content = section.querySelector('[data-slot="content"]');

    dialog.el = section.querySelector('[data-role="dialog"]');
    dialog.title = section.querySelector('[data-slot="dialog-title"]');
    dialog.body = section.querySelector('[data-slot="dialog-body"]');
    dialog.note = section.querySelector('[data-slot="dialog-note"]');
    dialog.confirm = section.querySelector('[data-role="dialog-confirm"]');
    dialog.cancel = section.querySelector('[data-role="dialog-cancel"]');
    dialog.close = section.querySelector('[data-role="dialog-close"]');

    return {
      section: section,
      loading: section.querySelector('[data-slot="loading"]'),
      error: section.querySelector('[data-slot="error"]'),
      empty: section.querySelector('[data-slot="empty"]'),
      revoked: section.querySelector('[data-slot="revoked"]'),
      content: content,
      accessMode: content.querySelector('[data-slot="access-mode"]'),
      accessHint: content.querySelector('[data-slot="access-hint"]'),
      accessToggle: content.querySelector('[data-role="access-toggle"]'),
      addPerson: content.querySelector('[data-role="add-person"]'),
      search: content.querySelector('[data-role="search"]'),
      peopleLoading: content.querySelector('[data-slot="people-loading"]'),
      peopleError: content.querySelector('[data-slot="people-error"]'),
      peopleEmpty: content.querySelector('[data-slot="people-empty"]'),
      peopleList: content.querySelector('[data-slot="people-list"]'),
      peopleMore: content.querySelector('[data-role="people-more"]'),
      statusGrid: content.querySelector('[data-slot="status-grid"]'),
      deniedCard: content.querySelector('[data-card="denied"]'),
      deniedList: content.querySelector('[data-slot="denied-list"]'),
    };
  }

  /* Dialog --------------------------------------------------------------- */

  function refreshConfirm() {
    if (!dialog.el) {
      return;
    }
    var emptyField =
      dialog.field && typeof dialog.field.value === "string" ? normalize(dialog.field.value) === "" : false;
    dialog.confirm.disabled = dialog.pending || emptyField || dialog.locked === true;
    dialog.cancel.disabled = dialog.pending;
    dialog.close.disabled = dialog.pending;
  }

  function setDialogPending(pending) {
    dialog.pending = pending;
    dialog.confirm.textContent = pending ? dialog.pendingLabel : dialog.confirmLabel;
    refreshConfirm();
  }

  function clearDialogNote() {
    if (!dialog.note) {
      return;
    }
    dialog.note.textContent = "";
    dialog.note.hidden = true;
  }

  function showDialogNote(message) {
    if (!dialog.note) {
      return;
    }
    dialog.note.textContent = String(message || "");
    dialog.note.hidden = false;
  }

  function openDialog(config) {
    dialog.title.textContent = config.title;
    dialog.body.textContent = "";
    dialog.body.appendChild(config.content);
    clearDialogNote();

    dialog.field = config.field || null;
    dialog.locked = config.confirmDisabled === true;
    dialog.onConfirm = config.onConfirm;
    dialog.confirmLabel = config.confirmLabel || "Save";
    dialog.pendingLabel = config.pendingLabel || "Saving\u2026";
    dialog.pending = false;

    dialog.confirm.textContent = dialog.confirmLabel;
    dialog.confirm.className = config.danger ? "btn is-danger" : "btn";
    dialog.cancel.disabled = false;
    dialog.close.disabled = false;
    dialog.confirm.disabled = dialog.locked;

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
    clearDialogNote();

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
      toastError(err, "Something went wrong. Please try again.");
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
    }
  }

  /* Events --------------------------------------------------------------- */

  function onPeopleClick(event) {
    var target = event.target;
    var button =
      target && typeof target.closest === "function" ? target.closest("button[data-action]") : null;
    if (!button) {
      return;
    }

    var item = findPerson(button.getAttribute("data-user-id"));
    if (!item) {
      return;
    }

    var action = button.getAttribute("data-action");
    if (action === "edit") {
      openEditDialog(item);
    } else if (action === "revoke") {
      openRevokeDialog(item);
    }
  }

  function onDeniedClick(event) {
    var target = event.target;
    var button =
      target && typeof target.closest === "function" ? target.closest("button[data-action]") : null;
    if (!button) {
      return;
    }
    if (button.getAttribute("data-action") === "allow") {
      allowDenied(button.getAttribute("data-user-id"));
    }
  }

  function bind() {
    els.accessToggle.addEventListener("change", onAccessToggle);
    els.addPerson.addEventListener("click", openAddDialog);
    els.peopleList.addEventListener("click", onPeopleClick);
    els.deniedList.addEventListener("click", onDeniedClick);
    els.peopleMore.addEventListener("click", function () {
      if (people.status === "ready") {
        loadPeople(false);
      }
    });

    // Debounced so typing issues one server-side query instead of one per key.
    var runSearch = core.debounce(function () {
      people.q = normalize(els.search.value);
      loadPeople(true);
    }, SEARCH_DEBOUNCE_MS);
    els.search.addEventListener("input", runSearch);

    dialog.close.addEventListener("click", closeDialog);
    dialog.cancel.addEventListener("click", closeDialog);
    dialog.confirm.addEventListener("click", confirmDialog);
    dialog.el.addEventListener("click", function (event) {
      if (event.target === dialog.el) {
        closeDialog();
      }
    });

    document.addEventListener("keydown", onDialogKeydown);
  }

  /* Registration --------------------------------------------------------- */

  try {
    core.registerView(VIEW, { mount: mount });
    coreRoutesSettings = true;
  } catch (err) {
    // The shell's route table is fixed and does not know this view; the
    // adapter path above takes over instead of failing the whole script.
    coreRoutesSettings = false;
  }

  if (!coreRoutesSettings) {
    // Installed while the document is still parsing: core.js binds its own
    // hashchange listener in init (on DOMContentLoaded), so being first is what
    // lets this handler claim #/settings before the shell rewrites it.
    installAdapter();
  }

  document.addEventListener("DOMContentLoaded", init);
})(window);
