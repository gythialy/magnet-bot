/*
 * keywords.js — the read-only Keywords view.
 *
 * Renders the user's project keywords inside #view-keywords and gives the page
 * its own toolbar, list and state handling. js/core.js owns the shell (routing,
 * Telegram bootstrap, transport); this file only knows what a keyword is.
 *
 * States: loading -> ready (list or empty) | error (retry). The search field
 * filters the already loaded list in place; it never issues a request, because
 * the bot's keyword sets are small and a round trip per keystroke would only add
 * latency and load.
 *
 * P2 is read-only: "+ Add", edit and delete are rendered but disabled so the
 * layout matches the next release without pretending to work now.
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

  var els = null;
  var items = [];
  var query = "";
  var status = "idle";
  var loaded = false;

  var TEMPLATE =
    '<div class="kw-toolbar">' +
    '<input class="search-input" type="search" data-role="search" placeholder="Search keywords" ' +
    'aria-label="Search keywords" autocomplete="off" />' +
    '<button type="button" class="btn kw-add" disabled title="Available in the next release">+ Add</button>' +
    "</div>" +
    '<div class="skeleton" data-slot="loading">Loading\u2026</div>' +
    '<div class="error" data-slot="error" role="alert" hidden></div>' +
    '<div class="empty" data-slot="empty" hidden></div>' +
    '<ul class="kw-list" data-slot="list" hidden></ul>';

  function api() {
    var apiNamespace = global.MagnetAPI;
    if (!apiNamespace) {
      throw new Error("MagnetAPI is not loaded");
    }
    return apiNamespace;
  }

  /* Helpers -------------------------------------------------------------- */

  // Thousands separators without pulling in Intl: the counter is an integer and
  // the grouping is the only thing a list needs. Negative values cannot come
  // from the API, but keeping the sign makes the helper predictable.
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

  /* Rendering ------------------------------------------------------------ */

  function actionButton(label, glyph) {
    return (
      '<button type="button" class="icon-btn" disabled title="Available in the next release" ' +
      'aria-label="' +
      core.escapeHTML(label) +
      '">' +
      glyph +
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
      actionButton("Edit keyword", "\u270f\ufe0f") +
      actionButton("Delete keyword", "\ud83d\uddd1") +
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
          ? "No keywords yet. Add keywords from the bot, then reopen this page."
          : 'No keywords match "' + query.trim() + '".';
    }

    if (showList) {
      els.list.innerHTML = visible.map(rowHTML).join("");
    }
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

    return {
      search: section.querySelector('[data-role="search"]'),
      loading: section.querySelector('[data-slot="loading"]'),
      error: section.querySelector('[data-slot="error"]'),
      empty: section.querySelector('[data-slot="empty"]'),
      list: section.querySelector('[data-slot="list"]'),
    };
  }

  function bind() {
    els.search.addEventListener("input", function () {
      query = els.search.value;
      render();
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

    // Load once per session: nothing in P2 can change keyword rows, so a tab
    // switch reuses the cache. A later write phase invalidates the view and the
    // same check turns that into a reload on the next visit.
    if (!loaded || core.state.isStale(VIEW)) {
      return load();
    }
  }

  core.registerView(VIEW, { mount: mount });
})(window);
