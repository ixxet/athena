// ==UserScript==
// @name         Ashton TouchNet Edge Bridge (Template Legacy)
// @namespace    https://github.com/ixxet/athena
// @version      0.3.0
// @description  Legacy-safe template for older ChromeOS / Tampermonkey environments. Replace the placeholder config values before use.
// @match        *://*/*
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
    // https://athena-edge.example.com
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
    retryIntervalMs: 5000,
    readyPollMs: 1000,
    focusPollMs: 1000,
    seenStorageKey: 'ashton.edge.template.legacy.seen',
    queueStorageKey: 'ashton.edge.template.legacy.queue'
  };

  var state = {
    observer: null,
    readyTimer: null,
    retryTimer: null,
    focusTimer: null,
    banner: null,
    seen: loadStringArray(CONFIG.seenStorageKey),
    queue: loadQueue(CONFIG.queueStorageKey)
  };

  function loadStringArray(key) {
    try {
      var raw = window.localStorage.getItem(key);
      var parsed = JSON.parse(raw || '[]');
      return Array.isArray(parsed) ? parsed : [];
    } catch (error) {
      console.warn('ASHTON edge legacy: failed to load array', key, error);
      return [];
    }
  }

  function persistSeen() {
    while (state.seen.length > CONFIG.seenLimit) {
      state.seen.shift();
    }
    window.localStorage.setItem(CONFIG.seenStorageKey, JSON.stringify(state.seen));
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
      console.warn('ASHTON edge legacy: failed to load retry queue', error);
      return [];
    }
  }

  function persistQueue() {
    window.localStorage.setItem(CONFIG.queueStorageKey, JSON.stringify(state.queue));
  }

  function queueContains(eventId) {
    var i;
    for (i = 0; i < state.queue.length; i += 1) {
      if (state.queue[i] && state.queue[i].event_id === eventId) {
        return true;
      }
    }
    return false;
  }

  function queuePayload(payload) {
    if (!queueContains(payload.event_id)) {
      state.queue.push(payload);
      persistQueue();
      console.warn('ASHTON edge legacy: queued payload for retry', payload);
    }
  }

  function simpleHash(input) {
    var hash = 2166136261;
    var i;
    for (i = 0; i < input.length; i += 1) {
      hash ^= input.charCodeAt(i);
      hash += (hash << 1) + (hash << 4) + (hash << 7) + (hash << 8) + (hash << 24);
    }
    if (hash < 0) {
      hash = hash >>> 0;
    }
    return ('00000000' + hash.toString(16)).slice(-8);
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
    if (!state.queue.length) {
      return;
    }

    console.info('ASHTON edge legacy: retrying queued payloads', state.queue.length);

    var remaining = [];
    var index = 0;

    function processNext() {
      var payload;
      if (index >= state.queue.length) {
        state.queue = remaining;
        persistQueue();
        return;
      }

      payload = state.queue[index];
      index += 1;

      postPayload(payload, function (response) {
        rememberEvent(payload.event_id);
        console.info('ASHTON edge legacy: queued payload accepted', payload.event_id, response);
        processNext();
      }, function (error) {
        console.warn('ASHTON edge legacy: queued retry failed', error);
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
    if (hasSeen(eventId) || queueContains(eventId)) {
      console.info('ASHTON edge legacy: duplicate row ignored', eventId);
      return;
    }

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

    console.info('ASHTON edge legacy: posting row', payload);
    postPayload(payload, function (response) {
      rememberEvent(eventId);
      console.info('ASHTON edge legacy: payload accepted', payload, response);
    }, function (error) {
      console.warn('ASHTON edge legacy: post failed, queueing for retry', error);
      queuePayload(payload);
    });
  }

  function scanRows(root) {
    var rows;
    var i;

    if (!root || !root.querySelectorAll) {
      return;
    }

    rows = root.querySelectorAll('tr');
    for (i = 0; i < rows.length; i += 1) {
      processRow(rows[i]);
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
        var i;
        var j;
        var node;
        for (i = 0; i < mutations.length; i += 1) {
          for (j = 0; j < mutations[i].addedNodes.length; j += 1) {
            node = mutations[i].addedNodes[j];
            if (node && node.nodeType === 1 && node.matches && node.matches('tr')) {
              processRow(node);
            } else if (node && node.nodeType === 1) {
              scanRows(node);
            }
          }
        }
      });

      state.observer.observe(tableBody, { childList: true, subtree: true });
      console.info('ASHTON edge legacy: observing table body', CONFIG.tableBodySelector);
    } else {
      console.warn('ASHTON edge legacy: MutationObserver unavailable, polling existing rows only');
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
    state.focusTimer = window.setInterval(updateFocusBanner, CONFIG.focusPollMs);
  }

  console.info('ASHTON edge legacy: script active', window.location.href, CONFIG.nodeId);
  drainQueue();

  if (!attachObserver()) {
    console.warn('ASHTON edge legacy: table not found yet, polling');
    startPolling();
  } else {
    state.retryTimer = window.setInterval(drainQueue, CONFIG.retryIntervalMs);
    state.focusTimer = window.setInterval(updateFocusBanner, CONFIG.focusPollMs);
  }

  updateFocusBanner();
})();
