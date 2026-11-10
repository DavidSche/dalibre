// The host's side of a plugin's world, run as a script in its runtime before
// the plugin. It turns three host functions that pass strings (JSON) into
// the dalibre object and console, and calls the plugin's exports for the
// host, reporting each outcome back as JSON. The host functions are taken
// off the global object, so only this code can call them. Its value is the
// two functions the host calls: { load, call }. Everything else a plugin
// uses (fetch, timers, URL, node:fs…) is the engine's standard API.
(() => {
  "use strict";
  const hostLog = __host_log, hostSettle = __host_settle, hostInfo = __host_info;
  for (const k of ["__host_log", "__host_settle", "__host_info"]) delete globalThis[k];
  const { parse, stringify } = JSON;
  const { freeze, defineProperty, keys } = Object;
  const resolved = Promise.resolve.bind(Promise), rejected = Promise.reject.bind(Promise);

  const { api, plugin, settings, notAllowed } = parse(hostInfo());

  // fetchKind is the error kind of what fetch rejected with: permission
  // for a request the plugin may not make, timeout when it was aborted,
  // unavailable for a network error; "" for anything else.
  function fetchKind(e) {
    if (!(e instanceof Error)) return "";
    if (String(e.cause?.message ?? "").includes(notAllowed)) return "permission";
    if (e.name === "AbortError" || e.name === "TimeoutError") return "timeout";
    if (e.name === "TypeError" && (e.message === "fetch failed" || e.message === "terminated")) return "unavailable";
    return "";
  }

  // fetchError makes what fetch rejected with, uncaught, an error of its
  // kind, with the reason (its cause) as the message.
  function fetchError(e) {
    const kind = fetchKind(e);
    if (!kind) return e;
    const err = new Error(String(e.cause?.message ?? e.message));
    err.name = "DalibreError";
    err.kind = kind;
    return err;
  }

  const log = (level) => (...args) => {
    hostLog(level, args.map((a) => {
      if (typeof a === "string") return a;
      try { return a instanceof Error ? String(a) : stringify(a) ?? String(a); } catch { return String(a); }
    }).join(" "));
  };

  const dalibre = freeze({
    api,
    plugin: freeze(plugin),
    settings: freeze(settings),
    error(kind, message) {
      const e = new Error(message === undefined ? String(kind) : String(message));
      e.name = "DalibreError";
      e.kind = String(kind);
      return e;
    },
  });
  const fixed = (name, value) => defineProperty(globalThis, name, { value, writable: false, configurable: false });
  fixed("dalibre", dalibre);
  fixed("console", freeze({ log: log("log"), info: log("info"), warn: log("warn"), error: log("error"), debug: log("debug") }));

  // self is the global object, as in a Web Worker: libraries made for
  // browsers look for it.
  globalThis.self = globalThis;

  // CommonJS, for plugins that set module.exports or exports.x.
  globalThis.module = { exports: {} };
  globalThis.exports = globalThis.module.exports;

  function describe(e) {
    if (e instanceof Error) {
      if (typeof e.kind !== "string" && fetchKind(e)) e = fetchError(e); // a fetch the script didn't catch
      return { name: String(e.name), message: String(e.message), stack: String(e.stack ?? ""), kind: typeof e.kind === "string" ? e.kind : "" };
    }
    let message;
    try { message = String(e); } catch { message = "a value that isn’t an Error"; }
    return { name: "", message, stack: "", kind: "" };
  }

  function report(promise) {
    promise.then((value) => {
      let s;
      try {
        s = stringify({ ok: true, value: value === undefined ? null : value });
      } catch (e) {
        s = stringify({ ok: false, error: { name: "", kind: "invalid", stack: "", message: "returned what isn’t JSON: " + describe(e).message } });
      }
      hostSettle(s);
    }, (e) => hostSettle(stringify({ ok: false, error: describe(e) })));
  }

  let exported = {};

  // __dalibre_load imports the plugin's main module and reports the names
  // of the functions it exports: ES module exports, else an exported
  // default object, else module.exports.
  fixed("__dalibre_load", (main) => {
    report(import("./" + main).then((m) => {
      const named = keys(m).filter((k) => k !== "default");
      if (named.length > 0) exported = m;
      else if (m.default && typeof m.default === "object") exported = m.default;
      else exported = globalThis.module.exports ?? {};
      return keys(exported).filter((k) => typeof exported[k] === "function");
    }));
  });

  // __dalibre_call calls an export with arguments given as a JSON array.
  fixed("__dalibre_call", (name, args) => {
    let p;
    try {
      p = resolved(exported[name](...parse(args)));
    } catch (e) {
      p = rejected(e);
    }
    report(p);
  });

  return { load: globalThis.__dalibre_load, call: globalThis.__dalibre_call };
})();
