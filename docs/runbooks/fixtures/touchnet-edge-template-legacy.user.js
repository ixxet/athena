// ==UserScript==
// @name         Ashton TouchNet Edge Bridge (Template Legacy)
// @namespace    https://github.com/ixxet/athena
// @version      0.4.0
// @description  Legacy-safe template for older ChromeOS / Tampermonkey environments. Replace the placeholder config values before use.
// @match        *://securetouchnet.net/*
// @match        *://*.securetouchnet.net/*
// @match        *://secure.touchnet.net/*
// @match        *://*.secure.touchnet.net/*
// @match        *://*.touchnet.net/*
// @match        file://*/*
// @grant        GM_xmlhttpRequest
// @connect      127.0.0.1
// @connect      localhost
// @connect      *
// @run-at       document-end
// ==/UserScript==

(function () {
  'use strict';

  var CONFIG = {
    // Replace with the real public ATHENA ingress URL, for example:
    // https://tap.lintellabs.net
    baseUrl: 'https://REPLACE-ATHENA-EDGE.example.com',

    // Workstation identity only. Do not encode entry/exit here.
    nodeId: 'REPLACE_NODE_ID',

    // Active edge token for the workstation node above.
    token: 'REPLACE_EDGE_TOKEN',

    // Truthful facility and zone values for the workstation location.
    facilityId: 'REPLACE_FACILITY_ID',
    zoneId: 'REPLACE_ZONE_ID',

    defaultDirection: 'in',
    tableBodySelector: '#verify-account-access-transaction-list > tbody',
    inputSelector: '#verify_account_number',
    seenLimit: 200,
    retryIntervalMs: 15000,
    readyPollMs: 3000,
    focusPollMs: 2000,
    showFocusBanner: false,
    debug: false
  };

  var LOG_PREFIX = 'ASHTON edge legacy';
  var INSTANCE_KEY = '__ashtonEdgeBridgeNodeId__';

  function shouldActivateOnThisPage() {
    if (window.location.protocol === 'file:') {
      return true;
    }

    return window.location.hostname.toLowerCase().indexOf('touchnet') !== -1;
  }

  function hasPlaceholderConfig() {
    var values = [
      CONFIG.baseUrl,
      CONFIG.nodeId,
      CONFIG.token,
      CONFIG.facilityId,
      CONFIG.zoneId
    ];
    var index;

    for (index = 0; index < values.length; index += 1) {
      if (String(values[index]).indexOf('REPLACE_') !== -1 || String(values[index]).trim() === 'replace-me') {
        return true;
      }
    }

    return false;
  }

  function debugLog() {
    if (!CONFIG.debug) {
      return;
    }
    console.info.apply(console, [LOG_PREFIX + ':'].concat(Array.prototype.slice.call(arguments)));
  }

  function warnLog() {
    console.warn.apply(console, [LOG_PREFIX + ':'].concat(Array.prototype.slice.call(arguments)));
  }

  if (!shouldActivateOnThisPage()) {
    return;
  }

  if (hasPlaceholderConfig()) {
    warnLog('placeholder config detected; script is idle until values are replaced');
    return;
  }

  if (window[INSTANCE_KEY] && window[INSTANCE_KEY] !== CONFIG.nodeId) {
    warnLog('another workstation script is already active on this page', window[INSTANCE_KEY]);
    return;
  }
  window[INSTANCE_KEY] = CONFIG.nodeId;

  var storagePrefix = 'ashton.edge.' + CONFIG.nodeId.replace(/[^a-z0-9_-]/gi, '_').toLowerCase();

  var state = {
    observer: null,
    readyTimer: null,
    retryTimer: null,
    focusTimer: null,
    banner: null,
    drainingQueue: false,
    pendingEventIds: {},
    seen: loadStringArray(storageKey('seen')),
    queue: loadQueue(storageKey('queue'))
  };

  function storageKey(suffix) {
    return storagePrefix + '.' + suffix;
  }

  function loadStringArray(key) {
    try {
      var raw = window.localStorage.getItem(key);
      var parsed = JSON.parse(raw || '[]');
      return Array.isArray(parsed) ? parsed : [];
    } catch (error) {
      warnLog('failed to load array', key, error);
      return [];
    }
  }

  function persistSeen() {
    while (state.seen.length > CONFIG.seenLimit) {
      state.seen.shift();
    }
    window.localStorage.setItem(storageKey('seen'), JSON.stringify(state.seen));
  }

  function hasSeen(eventId) {
    return state.seen.indexOf(eventId) !== -1;
  }

  function rememberEvent(eventId) {
    if (hasSeen(eventId)) {
      return;
    }
    state.seen.push(eventId);
    persistSeen();
  }

  function loadQueue(key) {
    try {
      var parsed = JSON.parse(window.localStorage.getItem(key) || '[]');
      return Array.isArray(parsed) ? parsed : [];
    } catch (error) {
      warnLog('failed to load retry queue', error);
      return [];
    }
  }

  function persistQueue() {
    window.localStorage.setItem(storageKey('queue'), JSON.stringify(state.queue));
  }

  function queueContains(eventId) {
    var index;
    for (index = 0; index < state.queue.length; index += 1) {
      if (state.queue[index] && state.queue[index].event_id === eventId) {
        return true;
      }
    }
    return false;
  }

  function queuePayload(payload) {
    if (!queueContains(payload.event_id)) {
      state.queue.push(payload);
      persistQueue();
      warnLog('queued payload for retry', payload.event_id);
    }
  }

  function simpleHash(input) {
    var hash = 2166136261;
    var index;
    for (index = 0; index < input.length; index += 1) {
      hash ^= input.charCodeAt(index);
      hash += (hash << 1) + (hash << 4) + (hash << 7) + (hash << 8) + (hash << 24);
    }
    return ('00000000' + (hash >>> 0).toString(16)).slice(-8);
  }

  function deriveEventId(rowData) {
    var material = [
      CONFIG.nodeId,
      rowData.direction,
      rowData.result,
      (rowData.accountRaw || '').replace(/^\s+|\s+$/g, ''),
      rowData.observedAt,
      rowData.statusMessage || '',
      rowData.accountType || ''
    ].join('|');

    return 'edge-' + simpleHash(material + '|a') + simpleHash(material + '|b');
  }

  function textContent(node) {
    return node && node.textContent ? String(node.textContent).replace(/^\s+|\s+$/g, '') : '';
  }

  function readCellText(row, index) {
    var cell = row.children[index];
    var label;
    if (!cell) {
      return '';
    }
    label = cell.querySelector ? cell.querySelector('.verify-table-text') : null;
    return textContent(label || cell);
  }

  function parseObservedAt(timestampText) {
    var match = timestampText.match(/^(\d{4})[/-](\d{2})[/-](\d{2})\s+(\d{2}):(\d{2})(?::(\d{2}))?$/);
    var parsed;
    if (match) {
      parsed = new Date(
        Number(match[1]),
        Number(match[2]) - 1,
        Number(match[3]),
        Number(match[4]),
        Number(match[5]),
        Number(match[6] || '00')
      );
      if (!isNaN(parsed.getTime())) {
        return parsed.toISOString();
      }
    }

    parsed = new Date(timestampText);
    if (!isNaN(parsed.getTime())) {
      return parsed.toISOString();
    }

    return new Date().toISOString();
  }

  function extractDirection(statusMessage) {
    var normalized = String(statusMessage || '').toLowerCase();
    var selectedType;

    if (normalized.indexOf('exit') !== -1) {
      return 'out';
    }
    if (normalized.indexOf('entry') !== -1) {
      return 'in';
    }

    selectedType = document.querySelector("input[name='verify_trans_type']:checked");
    if (selectedType) {
      return selectedType.value === '2' ? 'out' : 'in';
    }

    return CONFIG.defaultDirection;
  }

  function extractRowData(row) {
    var passed;
    var failed;
    var accountRaw;

    if (!row || !row.children || row.children.length < 7) {
      return null;
    }

    passed = !!(row.querySelector && row.querySelector('.verify-pass'));
    failed = !!(row.querySelector && row.querySelector('.verify-fail'));
    if (!passed && !failed) {
      return null;
    }

    accountRaw = readCellText(row, 1);
    if (!accountRaw) {
      return null;
    }

    return {
      accountRaw: accountRaw,
      accountType: readCellText(row, 2),
      name: readCellText(row, 3),
      observedAt: parseObservedAt(readCellText(row, 4)),
      direction: extractDirection(readCellText(row, 5)),
      result: passed ? 'pass' : 'fail',
      statusMessage: readCellText(row, 5)
    };
  }

  function postPayload(payload, onSuccess, onFailure) {
    var url = CONFIG.baseUrl.replace(/\/$/, '') + '/api/v1/edge/tap';

    if (typeof GM_xmlhttpRequest === 'function') {
      GM_xmlhttpRequest({
        method: 'POST',
        url: url,
        timeout: 10000,
        headers: {
          'Content-Type': 'application/json',
          'X-Ashton-Edge-Token': CONFIG.token
        },
        data: JSON.stringify(payload),
        onload: function (response) {
          var parsed = null;
          if (response.status >= 200 && response.status < 300) {
            try {
              parsed = response.responseText ? JSON.parse(response.responseText) : null;
            } catch (error) {
              parsed = { raw: response.responseText };
            }
            onSuccess(parsed);
            return;
          }
          onFailure(new Error('ATHENA edge returned ' + response.status + ': ' + (response.responseText || '')));
        },
        onerror: function () {
          onFailure(new Error('ATHENA edge request failed before a response was received'));
        },
        ontimeout: function () {
          onFailure(new Error('ATHENA edge request timed out'));
        }
      });
      return;
    }

    try {
      var request = new XMLHttpRequest();
      request.open('POST', url, true);
      request.setRequestHeader('Content-Type', 'application/json');
      request.setRequestHeader('X-Ashton-Edge-Token', CONFIG.token);
      request.onreadystatechange = function () {
        var body;
        if (request.readyState !== 4) {
          return;
        }
        if (request.status >= 200 && request.status < 300) {
          try {
            onSuccess(request.responseText ? JSON.parse(request.responseText) : null);
          } catch (error) {
            onSuccess({ raw: request.responseText });
          }
          return;
        }
        body = request.responseText || '';
        onFailure(new Error('ATHENA edge returned ' + request.status + ': ' + body));
      };
      request.send(JSON.stringify(payload));
    } catch (error) {
      onFailure(error);
    }
  }

  function drainQueue() {
    if (state.drainingQueue || !state.queue.length) {
      return;
    }

    state.drainingQueue = true;

    var remaining = [];
    var index = 0;

    function finish() {
      state.queue = remaining;
      persistQueue();
      state.drainingQueue = false;
    }

    function processNext() {
      var payload;
      if (index >= state.queue.length) {
        finish();
        return;
      }

      payload = state.queue[index];
      index += 1;

      postPayload(payload, function () {
        rememberEvent(payload.event_id);
        processNext();
      }, function (error) {
        warnLog('queued retry failed', payload.event_id, error && error.message ? error.message : error);
        remaining.push(payload);
        processNext();
      });
    }

    processNext();
  }

  function processRow(row) {
    var rowData = extractRowData(row);
    var eventId;
    var payload;

    if (!rowData) {
      return;
    }

    eventId = deriveEventId(rowData);
    if (hasSeen(eventId) || queueContains(eventId) || state.pendingEventIds[eventId]) {
      return;
    }

    state.pendingEventIds[eventId] = true;
    payload = {
      event_id: eventId,
      account_raw: rowData.accountRaw,
      direction: rowData.direction,
      facility_id: document.body && document.body.dataset && document.body.dataset.facilityId ? document.body.dataset.facilityId : CONFIG.facilityId,
      zone_id: document.body && document.body.dataset && document.body.dataset.zoneId ? document.body.dataset.zoneId : CONFIG.zoneId,
      node_id: CONFIG.nodeId,
      observed_at: rowData.observedAt,
      result: rowData.result,
      account_type: rowData.accountType,
      name: rowData.name,
      status_message: rowData.statusMessage
    };

    postPayload(payload, function () {
      rememberEvent(eventId);
      delete state.pendingEventIds[eventId];
    }, function (error) {
      warnLog('post failed, queueing for retry', eventId, error && error.message ? error.message : error);
      queuePayload(payload);
      delete state.pendingEventIds[eventId];
    });
  }

  function scanRows(root) {
    var rows;
    var index;

    if (!root) {
      return;
    }

    if (root.nodeType === 1 && root.matches && root.matches('tr')) {
      processRow(root);
      return;
    }

    if (!root.querySelectorAll) {
      return;
    }

    rows = root.querySelectorAll('tr');
    for (index = 0; index < rows.length; index += 1) {
      processRow(rows[index]);
    }
  }

  function attachObserver() {
    var tableBody = document.querySelector(CONFIG.tableBodySelector);

    if (!tableBody) {
      return false;
    }

    if (state.observer && state.observer.disconnect) {
      state.observer.disconnect();
    }

    if (typeof MutationObserver === 'function') {
      state.observer = new MutationObserver(function (mutations) {
        var mutationIndex;
        var nodeIndex;
        var node;
        for (mutationIndex = 0; mutationIndex < mutations.length; mutationIndex += 1) {
          for (nodeIndex = 0; nodeIndex < mutations[mutationIndex].addedNodes.length; nodeIndex += 1) {
            node = mutations[mutationIndex].addedNodes[nodeIndex];
            if (node && node.nodeType === 1) {
              scanRows(node);
            }
          }
        }
      });

      state.observer.observe(tableBody, { childList: true });
      debugLog('observing table body', CONFIG.tableBodySelector);
    } else {
      warnLog('MutationObserver unavailable, polling existing rows only');
    }

    scanRows(tableBody);
    return true;
  }

  function ensureBanner() {
    var banner;
    if (state.banner) {
      return state.banner;
    }

    banner = document.createElement('div');
    banner.id = 'ashton-edge-legacy-focus-banner';
    banner.style.position = 'fixed';
    banner.style.top = '12px';
    banner.style.right = '12px';
    banner.style.zIndex = '999999';
    banner.style.padding = '10px 12px';
    banner.style.borderRadius = '8px';
    banner.style.background = '#8a1c1c';
    banner.style.color = '#fff';
    banner.style.font = '600 13px/1.4 sans-serif';
    banner.style.boxShadow = '0 8px 24px rgba(0,0,0,0.2)';
    banner.style.display = 'none';
    banner.textContent = 'TouchNet input is not focused. Scanner wedges usually stop working until focus returns to the account field.';
    document.body.appendChild(banner);
    state.banner = banner;
    return banner;
  }

  function updateFocusBanner() {
    if (!CONFIG.showFocusBanner) {
      return;
    }

    var input = document.querySelector(CONFIG.inputSelector);
    var banner = ensureBanner();
    banner.style.display = input && document.activeElement !== input ? 'block' : 'none';
  }

  function startPolling() {
    state.readyTimer = window.setInterval(function () {
      if (attachObserver()) {
        window.clearInterval(state.readyTimer);
        state.readyTimer = null;
      }
    }, CONFIG.readyPollMs);

    state.retryTimer = window.setInterval(drainQueue, CONFIG.retryIntervalMs);
    if (CONFIG.showFocusBanner) {
      state.focusTimer = window.setInterval(updateFocusBanner, CONFIG.focusPollMs);
    }
  }

  function cleanup() {
    if (state.observer && state.observer.disconnect) {
      state.observer.disconnect();
    }
    if (state.readyTimer) {
      window.clearInterval(state.readyTimer);
    }
    if (state.retryTimer) {
      window.clearInterval(state.retryTimer);
    }
    if (state.focusTimer) {
      window.clearInterval(state.focusTimer);
    }
  }

  window.addEventListener('beforeunload', cleanup);

  drainQueue();
  debugLog('script active', window.location.href, CONFIG.nodeId);
  if (!attachObserver()) {
    startPolling();
  } else {
    state.retryTimer = window.setInterval(drainQueue, CONFIG.retryIntervalMs);
    if (CONFIG.showFocusBanner) {
      state.focusTimer = window.setInterval(updateFocusBanner, CONFIG.focusPollMs);
      updateFocusBanner();
    }
  }
})();
