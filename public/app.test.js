// Minimal failing test for app.js — asserts every method called on App
// is defined on App. Run: node public/app.test.js
//
// The simplest reproducible failure mode is: clicking the "保存 Cookie"
// button calls App.saveCookie(), which calls this._extractLegacyParts,
// which throws TypeError because the method was deleted in commit 076d381.
// Same TypeError fires for App.fillFromFullCookie (top-input onpaste),
// App.toggleCookieVisibility (show-cookie checkbox), App.copyEndpoint
// (📋 button on each endpoint), and App.init() (which calls loadLogs).

const fs = require('fs');
const path = require('path');
const vm = require('vm');

const code = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');

// Mock just enough of the browser to let app.js reach its endpoints.
const elements = {};
function makeEl(id) {
    if (!elements[id]) {
        elements[id] = {
            id, value: '',
            textContent: '', innerHTML: '',
            style: {},
            classList: { toggle() {}, add() {}, remove() {} },
            addEventListener() {}, appendChild() {}, removeChild() {},
            previousElementSibling: null, childNodes: [],
            placeholder: '',
            type: 'password',
            _focus: () => {},
            select() {},
        };
    }
    return elements[id];
}
const documentMock = {
    getElementById: (id) => makeEl(id),
    querySelectorAll: () => [],
    addEventListener: () => {},
    createElement: () => makeEl('div'),
};
const fakeFetch = () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}), text: () => Promise.resolve('') });

const sandbox = {
    document: documentMock,
    window: {},
    fetch: fakeFetch,
    setInterval: () => 0,
    setTimeout: (fn, ms) => 0,
    Promise,
    console,
    location: { reload: () => {} },
    alert: () => {},
    confirm: () => false,
    navigator: { clipboard: { writeText: () => Promise.resolve() } },
};
sandbox.window.document = documentMock;

vm.createContext(sandbox);
// `const App = {...}` lives in the script's lexical scope; expose it via
// a trailing statement that references `App` from that scope and assigns
// it to the sandbox's global. (vm contexts only auto-publish var globals,
// not lexical const.)
vm.runInContext(code + '\nthis.App = App;', sandbox);

const App = sandbox.App;
if (!App) { console.error('FAIL: App object not defined'); process.exit(1); }

// Methods referenced (called) on App via onclick/onchange/onpaste in
// index.html and via this.X(...) inside other App methods.
const requiredMethods = [
    'init', 'switchTab',
    'loadConfig', 'saveConfig', 'applyConfigToUI', 'saveAgentId',
    'checkStatus', 'loadStatus', 'loadEnv', 'loadLogs',
    'saveCookie', '_extractLegacyParts', 'fillFromFullCookie',
    'toggleCookieVisibility', 'copyEndpoint',
    'testAPI', 'checkCookie', 'toggleFeature',
    'saveConcurrency', '_pollUntilReadyAndReload', 'restartService',
];

const missing = requiredMethods.filter((m) => typeof App[m] !== 'function');
if (missing.length > 0) {
    console.error('FAIL: missing methods on App: ' + JSON.stringify(missing));
    process.exit(1);
}

// Also exercise init() to ensure it doesn't throw (saveCookie has this exact bug).
let initErr = null;
try {
    App.init();
} catch (e) { initErr = e; }
if (initErr) {
    console.error('FAIL: App.init() threw: ' + initErr.message);
    process.exit(1);
}

let saveCookieErr = null;
try {
    makeEl('yuanbaoHyTokenInput').value = 'tok';
    makeEl('yuanbaoHyUserInput').value  = 'usr';
    App.saveCookie();
} catch (e) { saveCookieErr = e; }
if (saveCookieErr) {
    console.error('FAIL: App.saveCookie() threw: ' + saveCookieErr.message);
    process.exit(1);
}

console.log('PASS: all required methods present; init/saveCookie do not throw');
