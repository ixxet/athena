// ==UserScript==
// @name         Ashton TouchNet Edge Bridge
// @namespace    https://github.com/ixxet/athena
// @version      0.2.0
// @description  Observe TouchNet transaction rows and forward successful taps to ATHENA edge ingress.
// @match        *://*/*
// @match        file://*/*
// @grant        GM_xmlhttpRequest
// @connect      127.0.0.1
// @connect      localhost
// @connect      *
// ==/UserScript==

(function () {
  'use strict';

  const CONFIG = {
    baseUrl: 'http://127.0.0.1:18090',
    nodeId: 'entry-node',
    token: 'replace-me',
    facilityId: 'ashtonbee',
    zoneId: 'gym-ash',
    defaultDirection: 'in',
    selectors: {
      tableBody: '#verify-account-access-transaction-list > tbody',
      input: '#verify_account_number',
    },
    seenLimit: 200,
    retryIntervalMs: 5000,
    readyPollMs: 1000,
    focusPollMs: 1000,
    storage: {
      seen: 'ashton.edge.seen',
      queue: 'ashton.edge.queue',
    },
  };

  const state = {
    observer: null,
    readyTimer: null,
    retryTimer: null,
    focusTimer: null,
    banner: null,
    seen: loadSeen(),
    queue: loadQueue(),
  };

  function loadSeen() {
    try {
      const raw = window.localStorage.getItem(CONFIG.storage.seen);
      const parsed = JSON.parse(raw || '[]');
      return new Set(Array.isArray(parsed) ? parsed : []);
    } catch (error) {
      console.warn('ASHTON edge: failed to load seen set', error);
      return new Set();
    }
  }

  function persistSeen() {
    const values = Array.from(state.seen).slice(-CONFIG.seenLimit);
    window.localStorage.setItem(CONFIG.storage.seen, JSON.stringify(values));
  }

  function rememberEvent(eventId) {
    if (state.seen.has(eventId)) {
      return;
    }

    state.seen.add(eventId);
    while (state.seen.size > CONFIG.seenLimit) {
      const oldest = state.seen.values().next().value;
      state.seen.delete(oldest);
    }
    persistSeen();
  }

  function loadQueue() {
    try {
      const parsed = JSON.parse(window.localStorage.getItem(CONFIG.storage.queue) || '[]');
      return Array.isArray(parsed) ? parsed : [];
    } catch (error) {
      console.warn('ASHTON edge: failed to load retry queue', error);
      return [];
    }
  }

  function persistQueue() {
    window.localStorage.setItem(CONFIG.storage.queue, JSON.stringify(state.queue));
  }

  function queuePayload(payload) {
    if (!state.queue.some((queued) => queued.event_id === payload.event_id)) {
      state.queue.push(payload);
      persistQueue();
      console.warn('ASHTON edge: queued payload for retry', payload);
    }
  }

  async function deriveEventId(accountRaw, observedAt, direction, result) {
    const material = `${CONFIG.nodeId}|${direction}|${result}|${accountRaw.trim()}|${observedAt}`;
    const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(material));
    const bytes = Array.from(new Uint8Array(digest)).slice(0, 16);
    const hex = bytes.map((value) => value.toString(16).padStart(2, '0')).join('');
    return `edge-${hex}`;
  }

  function postPayloadWithTampermonkey(url, payload) {
    return new Promise((resolve, reject) => {
      GM_xmlhttpRequest({
        method: 'POST',
        url,
        headers: {
          'Content-Type': 'application/json',
          'X-Ashton-Edge-Token': CONFIG.token,
        },
        data: JSON.stringify(payload),
        onload: (response) => {
          if (response.status >= 200 && response.status < 300) {
            try {
              resolve(response.responseText ? JSON.parse(response.responseText) : null);
            } catch (error) {
              resolve({ raw: response.responseText });
            }
            return;
          }

          reject(new Error(`ATHENA edge returned ${response.status}: ${response.responseText || ''}`));
        },
        onerror: () => {
          reject(new Error('ATHENA edge request failed before a response was received'));
        },
        ontimeout: () => {
          reject(new Error('ATHENA edge request timed out'));
        },
      });
    });
  }

  async function postPayload(payload) {
    const url = `${CONFIG.baseUrl.replace(/\/$/, '')}/api/v1/edge/tap`;

    if (typeof GM_xmlhttpRequest === 'function') {
      return postPayloadWithTampermonkey(url, payload);
    }

    const response = await fetch(url, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-Ashton-Edge-Token': CONFIG.token,
      },
      body: JSON.stringify(payload),
    });

    if (!response.ok) {
      const body = await response.text();
      throw new Error(`ATHENA edge returned ${response.status}: ${body}`);
    }

    try {
      return await response.json();
    } catch (error) {
      return null;
    }
  }

  async function drainQueue() {
    if (state.queue.length === 0) {
      return;
    }

    console.info('ASHTON edge: retrying queued payloads', state.queue.length);
    const remaining = [];
    for (const payload of state.queue) {
      try {
        const response = await postPayload(payload);
        rememberEvent(payload.event_id);
        console.info('ASHTON edge: queued payload accepted', payload.event_id, response);
      } catch (error) {
        console.warn('ASHTON edge: queued retry failed', error);
        remaining.push(payload);
      }
    }

    state.queue = remaining;
    persistQueue();
  }

  function textContent(node) {
    return (node ? node.textContent : '').trim();
  }

  function readCellText(row, index) {
    const cell = row.children[index];
    if (!cell) {
      return '';
    }

    const label = cell.querySelector('.verify-table-text');
    return textContent(label || cell);
  }

  function parseObservedAt(timestampText) {
    const match = timestampText.match(/^(\d{4})[/-](\d{2})[/-](\d{2})\s+(\d{2}):(\d{2})(?::(\d{2}))?$/);
    if (match) {
      const [, year, month, day, hour, minute, second = '00'] = match;
      const parsed = new Date(
        Number(year),
        Number(month) - 1,
        Number(day),
        Number(hour),
        Number(minute),
        Number(second),
      );
      if (!Number.isNaN(parsed.getTime())) {
        return parsed.toISOString();
      }
    }

    const parsed = new Date(timestampText);
    if (!Number.isNaN(parsed.getTime())) {
      return parsed.toISOString();
    }

    return new Date().toISOString();
  }

  function extractDirection(statusMessage) {
    const normalized = statusMessage.toLowerCase();
    if (normalized.includes('exit')) {
      return 'out';
    }

    if (normalized.includes('entry')) {
      return 'in';
    }

    const selectedType = document.querySelector("input[name='verify_trans_type']:checked");
    if (selectedType) {
      return selectedType.value === '2' ? 'out' : 'in';
    }

    return CONFIG.defaultDirection;
  }

  function extractRowData(row) {
    if (!(row instanceof HTMLElement) || row.children.length < 7) {
      return null;
    }

    const passed = Boolean(row.querySelector('.verify-pass'));
    const failed = Boolean(row.querySelector('.verify-fail'));
    if (!passed && !failed) {
      return null;
    }

    const accountRaw = readCellText(row, 1);
    if (!accountRaw) {
      return null;
    }

    const accountType = readCellText(row, 2);
    const name = readCellText(row, 3);
    const timestampText = readCellText(row, 4);
    const statusMessage = readCellText(row, 5);

    return {
      accountRaw,
      accountType,
      name,
      observedAt: parseObservedAt(timestampText),
      direction: extractDirection(statusMessage),
      result: passed ? 'pass' : 'fail',
      statusMessage,
    };
  }

  async function processRow(row) {
    const rowData = extractRowData(row);
    if (!rowData) {
      return;
    }

    console.info('ASHTON edge: observed pass row', rowData);
    const eventId = await deriveEventId(rowData.accountRaw, rowData.observedAt, rowData.direction, rowData.result);
    if (state.seen.has(eventId) || state.queue.some((queued) => queued.event_id === eventId)) {
      console.info('ASHTON edge: duplicate row ignored', eventId);
      return;
    }

    const payload = {
      event_id: eventId,
      account_raw: rowData.accountRaw,
      direction: rowData.direction,
      facility_id: document.body.dataset.facilityId || CONFIG.facilityId,
      zone_id: document.body.dataset.zoneId || CONFIG.zoneId,
      node_id: CONFIG.nodeId,
      observed_at: rowData.observedAt,
      result: rowData.result,
      account_type: rowData.accountType,
      name: rowData.name,
      status_message: rowData.statusMessage,
    };

    try {
      const response = await postPayload(payload);
      rememberEvent(eventId);
      console.info('ASHTON edge: payload accepted', payload, response);
    } catch (error) {
      console.warn('ASHTON edge: post failed, queueing for retry', error);
      queuePayload(payload);
    }
  }

  function scanRows(root) {
    const rows = root.querySelectorAll ? root.querySelectorAll('tr') : [];
    rows.forEach((row) => {
      void processRow(row);
    });
  }

  function attachObserver() {
    const tableBody = document.querySelector(CONFIG.selectors.tableBody);
    if (!tableBody) {
      return false;
    }

    if (state.observer) {
      state.observer.disconnect();
    }

    state.observer = new MutationObserver((mutations) => {
      mutations.forEach((mutation) => {
        mutation.addedNodes.forEach((node) => {
          if (node instanceof HTMLElement && node.matches('tr')) {
            void processRow(node);
            return;
          }

          if (node instanceof HTMLElement) {
            scanRows(node);
          }
        });
      });
    });

    state.observer.observe(tableBody, { childList: true, subtree: true });
    console.info('ASHTON edge: observing table body', CONFIG.selectors.tableBody);
    scanRows(tableBody);
    return true;
  }

  function ensureBanner() {
    if (state.banner) {
      return state.banner;
    }

    const banner = document.createElement('div');
    banner.id = 'ashton-edge-focus-banner';
    banner.style.position = 'fixed';
    banner.style.top = '12px';
    banner.style.right = '12px';
    banner.style.zIndex = '999999';
    banner.style.padding = '10px 12px';
    banner.style.borderRadius = '8px';
    banner.style.background = '#8a1c1c';
    banner.style.color = '#fff';
    banner.style.font = '600 13px/1.4 system-ui, sans-serif';
    banner.style.boxShadow = '0 8px 24px rgba(0,0,0,0.2)';
    banner.style.display = 'none';
    banner.textContent = 'TouchNet input is not focused. Scanner wedges usually stop working until focus returns to the account field.';
    document.body.appendChild(banner);
    state.banner = banner;
    return banner;
  }

  function updateFocusBanner() {
    const input = document.querySelector(CONFIG.selectors.input);
    const banner = ensureBanner();
    banner.style.display = input && document.activeElement !== input ? 'block' : 'none';
  }

  function startPolling() {
    state.readyTimer = window.setInterval(() => {
      if (attachObserver()) {
        window.clearInterval(state.readyTimer);
        state.readyTimer = null;
      }
    }, CONFIG.readyPollMs);

    state.retryTimer = window.setInterval(() => {
      void drainQueue();
    }, CONFIG.retryIntervalMs);

    state.focusTimer = window.setInterval(updateFocusBanner, CONFIG.focusPollMs);
  }

  void drainQueue();
  console.info('ASHTON edge: script active', window.location.href);
  if (!attachObserver()) {
    console.warn('ASHTON edge: table not found yet, polling');
    startPolling();
  } else {
    state.retryTimer = window.setInterval(() => {
      void drainQueue();
    }, CONFIG.retryIntervalMs);
    state.focusTimer = window.setInterval(updateFocusBanner, CONFIG.focusPollMs);
  }
  updateFocusBanner();
})();
