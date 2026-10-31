/*
 * api.js — the Mini App's JSON API surface.
 *
 * Thin, explicit wrappers over the documented /api endpoints. Every call goes
 * through MagnetApp.request, so initData authentication, JSON encoding, typed
 * errors and the 401 toast are implemented exactly once in core.js.
 *
 * Two conventions this file owns:
 *   - 204 responses (DELETE) resolve to null: core.js refuses to parse an empty
 *     body, and a caller that only needs "it worked" should not have to care.
 *   - the {id} path segment is URL-encoded once here, so a view never has to
 *     think about escaping an identifier it received from the API.
 */
(function (global) {
  "use strict";

  var ENDPOINTS = {
    statistics: "/api/statistics",
    notices: "/api/notices",
    keywords: "/api/keywords",
    alarms: "/api/alarms",
    alarmRecords: "/api/alarm-records",
    convert: "/api/convert",
  };

  function core() {
    var app = global.MagnetApp;
    if (!app || typeof app.request !== "function") {
      throw new Error("MagnetAPI: js/core.js must be loaded first");
    }
    return app;
  }

  // Empty values are dropped instead of serialised, so a view that passes
  // {q: ""} produces the same URL as one that omits it. That keeps the server
  // from having to treat "" and "absent" differently.
  function toQuery(params) {
    var search = new URLSearchParams();

    Object.keys(params || {}).forEach(function (key) {
      var value = params[key];
      if (value === undefined || value === null || value === "") {
        return;
      }
      search.set(key, String(value));
    });

    var encoded = search.toString();
    return encoded ? "?" + encoded : "";
  }

  function get(name, params) {
    return core().request(ENDPOINTS[name] + toQuery(params));
  }

  function send(method, path, body) {
    var options = { method: method };
    if (body !== undefined) {
      options.body = body;
    }
    return core().request(path, options);
  }

  // Item paths share the segment separator and the encoding rule; building them
  // in one place keeps "keywords/1" and "alarms/1" from drifting apart.
  function itemPath(collection, id) {
    return ENDPOINTS[collection] + "/" + encodeURIComponent(String(id));
  }

  global.MagnetAPI = {
    /* Read ---------------------------------------------------------------- */

    /** Counts plus top keywords for the Today summary bar. */
    statistics: function () {
      return get("statistics");
    },

    /** Notices for a date range: {range: "today"|"7d"|"all", q, offset, limit}. */
    notices: function (params) {
      return get("notices", params);
    },

    /** Project keywords. */
    keywords: function () {
      return get("keywords");
    },

    /** Alarm keywords. */
    alarms: function () {
      return get("alarms");
    },

    /** Alarm records: {q, offset, limit}. */
    alarmRecords: function (params) {
      return get("alarmRecords", params);
    },

    /* Write --------------------------------------------------------------- */

    /**
     * Add a comma-separated batch of project keywords.
     * Resolves {added: string[], skipped: string[]}.
     */
    createKeywords: function (keywords) {
      return send("POST", ENDPOINTS.keywords, { keywords: String(keywords) });
    },

    /** Rename one project keyword. Resolves {id, keyword}. */
    renameKeyword: function (id, keyword) {
      return send("PATCH", itemPath("keywords", id), { keyword: String(keyword) });
    },

    /** Remove one project keyword. Resolves null (204). */
    deleteKeyword: function (id) {
      return send("DELETE", itemPath("keywords", id));
    },

    /** Add a comma-separated batch of alarm keywords. Resolves {added, skipped}. */
    createAlarms: function (keywords) {
      return send("POST", ENDPOINTS.alarms, { keywords: String(keywords) });
    },

    /** Rename one alarm keyword. Resolves {id, keyword}. */
    renameAlarm: function (id, keyword) {
      return send("PATCH", itemPath("alarms", id), { keyword: String(keyword) });
    },

    /** Remove one alarm keyword. Resolves null (204). */
    deleteAlarm: function (id) {
      return send("DELETE", itemPath("alarms", id));
    },

    /* Conversion ---------------------------------------------------------- */

    /**
     * Ask the bot to convert one notice and send the file to the chat.
     * Resolves {status: "accepted", format} or {status: "converting"}; both mean
     * "submitted", and the file arrives as a chat message. 403 means the notice
     * is not convertible for this user, 429 is the per-user conversion budget.
     */
    convert: function (url, format) {
      return send("POST", ENDPOINTS.convert, { url: String(url), format: String(format) });
    },
  };
})(window);
