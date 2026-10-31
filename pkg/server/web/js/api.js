/*
 * api.js — the Mini App's JSON API surface.
 *
 * Thin, explicit wrappers over the documented /api endpoints. Every call goes
 * through MagnetApp.request, so initData authentication, JSON encoding, error
 * typing and the 401 toast are implemented exactly once in core.js.
 *
 * Read endpoints only: write operations (keyword/alarm CRUD, convert) arrive in
 * the next phase and must be added here rather than fetched ad hoc by a view.
 */
(function (global) {
  "use strict";

  var ENDPOINTS = {
    statistics: "/api/statistics",
    notices: "/api/notices",
    keywords: "/api/keywords",
    alarms: "/api/alarms",
    alarmRecords: "/api/alarm-records",
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

  global.MagnetAPI = {
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
  };
})(window);
