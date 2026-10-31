/*
 * core.js — the Mini App shell.
 *
 * Responsibilities, and nothing else:
 *   - Telegram bootstrap (ready/expand, themeParams -> CSS variables)
 *   - the "not opened from the bot" gate
 *   - hash routing between the three views
 *   - the transport helper every /api call goes through
 *   - small shared utilities (toast, openLink, escapeHTML, debounce)
 *
 * Page behaviour belongs to js/today.js, js/keywords.js and js/alarms.js; they
 * register themselves through MagnetApp.registerView so this file never needs
 * to know what a view renders.
 *
 * No build step, no third-party code, no external requests.
 */
(function () {
  "use strict";

  var DEFAULT_ROUTE = "today";
  var ROUTES = ["today", "keywords", "alarms"];
  var VIEW_TITLES = {
    today: "Today",
    keywords: "Keywords",
    alarms: "Alarms",
  };

  // Telegram themeParams key -> CSS custom property consumed by css/base.css.
  var THEME_VARIABLES = {
    bg_color: "--tg-bg",
    text_color: "--tg-text",
    hint_color: "--tg-hint",
    button_color: "--tg-button",
    button_text_color: "--tg-button-text",
    secondary_bg_color: "--tg-secondary-bg",
    link_color: "--tg-link",
  };

  // Telegram always sends every key in themeParams, but a dark client that
  // omits one must not leave a white surface behind.
  var DARK_FALLBACKS = {
    bg_color: "#17212b",
    text_color: "#f5f5f5",
    hint_color: "#7d8b99",
    button_color: "#5288c1",
    button_text_color: "#ffffff",
    secondary_bg_color: "#232e3c",
    link_color: "#6ab3f3",
  };

  var TOAST_TIMEOUT_MS = 3000;

  var views = new Map();

  var state = {
    initData: "",
    route: DEFAULT_ROUTE,
    stale: new Set(),
    /** Mark a view (or every view when called with no name) as needing a remount. */
    invalidate: function (name) {
      if (name) {
        state.stale.add(name);
      } else {
        ROUTES.forEach(function (route) {
          state.stale.add(route);
        });
      }
      return state;
    },
    /** True while the named view still owes a render. */
    isStale: function (name) {
      return state.stale.has(name);
    },
  };

  function telegram() {
    return window.Telegram && window.Telegram.WebApp ? window.Telegram.WebApp : null;
  }

  function byId(id) {
    return document.getElementById(id);
  }

  /* Theme ---------------------------------------------------------------- */

  function applyTheme() {
    var webApp = telegram();
    if (!webApp) {
      return;
    }

    var params = webApp.themeParams || {};
    var isDark = webApp.colorScheme === "dark";
    var root = document.documentElement;
    var fallbacks = isDark ? DARK_FALLBACKS : {};

    Object.keys(THEME_VARIABLES).forEach(function (key) {
      var value = params[key] || fallbacks[key];
      if (value) {
        root.style.setProperty(THEME_VARIABLES[key], value);
      }
    });

    root.setAttribute("data-theme", isDark ? "dark" : "light");
  }

  /* Gate ----------------------------------------------------------------- */

  function showGate() {
    var gate = byId("app-gate");
    var shell = byId("app-shell");
    if (shell) {
      shell.hidden = true;
    }
    if (gate) {
      gate.hidden = false;
    }
  }

  /* Routing -------------------------------------------------------------- */

  function normalizeRoute(hash) {
    return String(hash || "")
      .replace(/^#\/?/, "")
      .replace(/\/+$/, "");
  }

  function renderShell(name) {
    ROUTES.forEach(function (route) {
      var section = byId("view-" + route);
      if (section) {
        section.hidden = route !== name;
      }
    });

    var title = byId("app-title");
    if (title) {
      title.textContent = VIEW_TITLES[name] || VIEW_TITLES[DEFAULT_ROUTE];
    }

    var tabs = document.querySelectorAll("#tabbar [data-route]");
    Array.prototype.forEach.call(tabs, function (tab) {
      var tabRoute = normalizeRoute(tab.getAttribute("data-route") || tab.getAttribute("href"));
      tab.classList.toggle("is-active", tabRoute === name);
      if (tabRoute === name) {
        tab.setAttribute("aria-current", "page");
      } else {
        tab.removeAttribute("aria-current");
      }
    });
  }

  function mountView(name) {
    var view = views.get(name);
    if (!view || typeof view.mount !== "function") {
      // No script owns this view yet: the skeleton in index.html stays visible,
      // which is a truthful "nothing loaded" rather than a fake empty state.
      return;
    }

    // The flag is cleared after mount() ran, not before: a view decides whether
    // to reload by reading state.isStale(), so clearing it first would hide the
    // invalidation that a write just requested. A throwing mount keeps the flag
    // so the next visit retries instead of silently serving stale data.
    try {
      var result = view.mount();
      if (result && typeof result.then === "function") {
        result.catch(reportViewError);
      }
      state.stale.delete(name);
    } catch (err) {
      reportViewError(err);
    }
  }

  function reportViewError(err) {
    if (window.console && typeof window.console.error === "function") {
      window.console.error(err);
    }
    toast("Something went wrong. Please try again.", "error");
  }

  /**
   * Resolve the current hash, normalise it, keep the tab bar in sync and mount
   * the matching view. Unknown or missing hashes fall back to #/today.
   */
  function route() {
    var requested = normalizeRoute(window.location.hash);
    var name = ROUTES.indexOf(requested) === -1 ? DEFAULT_ROUTE : requested;

    state.route = name;

    // replaceState rather than assigning location.hash: assigning would append
    // a history entry per normalisation and re-enter this function.
    if (window.location.hash !== "#/" + name) {
      try {
        window.history.replaceState(null, "", "#/" + name);
      } catch (err) {
        // Some embedded webviews refuse history writes; routing still works.
        if (window.console && typeof window.console.error === "function") {
          window.console.error(err);
        }
      }
    }

    renderShell(name);
    mountView(name);

    return name;
  }

  function bindRouting() {
    window.addEventListener("hashchange", route);

    var tabs = document.querySelectorAll("#tabbar [data-route]");
    Array.prototype.forEach.call(tabs, function (tab) {
      tab.addEventListener("click", function () {
        // Let the browser update the hash; hashchange performs the render.
        if (normalizeRoute(tab.getAttribute("data-route")) === state.route) {
          route();
        }
      });
    });
  }

  /* Views ---------------------------------------------------------------- */

  /**
   * Register a view renderer: registerView("today", { mount: function () {...} }).
   * mount is called with no arguments whenever the route becomes active, and it
   * must be safe to call repeatedly.
   */
  function registerView(name, view) {
    if (ROUTES.indexOf(name) === -1) {
      throw new Error("MagnetApp: unknown view " + name);
    }
    if (!view || typeof view.mount !== "function") {
      throw new Error("MagnetApp: view " + name + " needs a mount function");
    }
    views.set(name, view);

    // A view script loaded after the initial route render still gets to draw.
    if (name === state.route) {
      mountView(name);
    }
  }

  /* Utilities ------------------------------------------------------------ */

  function toast(message, kind) {
    var container = byId("toasts") || document.body;
    var allowed = { info: true, success: true, error: true };
    var tone = allowed[kind] ? kind : "info";

    var el = document.createElement("div");
    el.className = "toast is-" + tone;
    el.setAttribute("role", "status");
    el.textContent = String(message == null ? "" : message);

    container.appendChild(el);

    window.setTimeout(function () {
      if (el.parentNode) {
        el.parentNode.removeChild(el);
      }
    }, TOAST_TIMEOUT_MS);

    return el;
  }

  function openLink(url) {
    var target = String(url == null ? "" : url);
    if (!target) {
      return;
    }

    var webApp = telegram();
    if (webApp && typeof webApp.openLink === "function") {
      webApp.openLink(target);
      return;
    }

    window.open(target, "_blank", "noopener");
  }

  function escapeHTML(value) {
    var replacements = {
      "&": "&amp;",
      "<": "&lt;",
      ">": "&gt;",
      '"': "&quot;",
      "'": "&#39;",
    };
    return String(value == null ? "" : value).replace(/[&<>"']/g, function (ch) {
      return replacements[ch];
    });
  }

  function debounce(fn, ms) {
    var wait = typeof ms === "number" ? ms : 250;
    var timer = null;

    return function debounced() {
      var args = arguments;
      var context = this;
      if (timer !== null) {
        window.clearTimeout(timer);
      }
      timer = window.setTimeout(function () {
        timer = null;
        fn.apply(context, args);
      }, wait);
    };
  }

  /* Transport ------------------------------------------------------------ */

  function requestError(response, message, code) {
    var err = new Error(message);
    err.status = response ? response.status : 0;
    err.code = code || "request_failed";
    return err;
  }

  // The API answers failures with {"error":{"code":"...","message":"..."}}.
  // Anything else (a proxy HTML page, an empty body) must still yield a typed
  // error rather than throwing a SyntaxError at the caller.
  function parseErrorBody(payload, response) {
    var detail = payload && payload.error ? payload.error : payload;
    var code = "http_" + response.status;
    var message = "Request failed (" + response.status + ")";

    if (detail && typeof detail === "object") {
      if (detail.code) {
        code = String(detail.code);
      }
      if (detail.message) {
        message = String(detail.message);
      }
    }

    return requestError(response, message, code);
  }

  /**
   * request("/api/notices", {method: "GET", body: {...}})
   *
   * Every call carries the raw initData header the server authenticates; the
   * body is JSON-encoded unless it is already a string. Non-2xx responses throw
   * an Error carrying status and code, and a 401 is additionally surfaced as a
   * toast because the session cannot recover without reopening the Mini App.
   */
  function request(path, options) {
    if (!state.initData) {
      return Promise.reject(requestError(null, "Open this app from the bot.", "unauthenticated"));
    }

    var opts = options || {};
    var headers = {
      "X-Telegram-Init-Data": state.initData,
      "Content-Type": "application/json",
    };
    var init = {
      method: opts.method || "GET",
      headers: headers,
      credentials: "same-origin",
      cache: "no-store",
    };

    if (opts.body !== undefined && opts.body !== null) {
      init.body = typeof opts.body === "string" ? opts.body : JSON.stringify(opts.body);
    }

    return fetch(path, init).then(function (response) {
      if (response.status === 401) {
        toast("Session expired, reopen from the bot", "error");
      }

      if (!response.ok) {
        return response
          .json()
          .then(function (payload) {
            throw parseErrorBody(payload, response);
          })
          .catch(function (err) {
            // A non-JSON error body rejects above with our typed error; anything
            // else (SyntaxError from response.json) becomes the generic error.
            if (err instanceof Error && err.status) {
              throw err;
            }
            throw parseErrorBody(null, response);
          });
      }

      if (response.status === 204) {
        return null;
      }

      var contentType = response.headers.get("Content-Type") || "";
      if (contentType.indexOf("application/json") === -1) {
        return response.text();
      }

      return response.json();
    });
  }

  /* Bootstrap ------------------------------------------------------------ */

  function init() {
    var webApp = telegram();
    var initData = webApp && typeof webApp.initData === "string" ? webApp.initData : "";

    state.initData = initData;

    if (!initData) {
      // Without credentials every request would be rejected, so stop before
      // binding routes or touching the network.
      showGate();
      return;
    }

    if (webApp) {
      if (typeof webApp.ready === "function") {
        webApp.ready();
      }
      if (typeof webApp.expand === "function") {
        webApp.expand();
      }
      applyTheme();
      if (typeof webApp.onEvent === "function") {
        webApp.onEvent("themeChanged", applyTheme);
      }
    }

    bindRouting();

    if (!window.location.hash) {
      try {
        window.history.replaceState(null, "", "#/" + DEFAULT_ROUTE);
      } catch (err) {
        if (window.console && typeof window.console.error === "function") {
          window.console.error(err);
        }
      }
    }

    route();
  }

  document.addEventListener("DOMContentLoaded", init);

  window.MagnetApp = {
    init: init,
    route: route,
    toast: toast,
    openLink: openLink,
    escapeHTML: escapeHTML,
    debounce: debounce,
    request: request,
    registerView: registerView,
    state: state,
  };
})();
