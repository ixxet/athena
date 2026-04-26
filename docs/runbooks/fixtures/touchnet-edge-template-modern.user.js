// ==UserScript==
// @name         Ashton TouchNet Edge Bridge (Template Modern)
// @namespace    https://github.com/ixxet/athena
// @version      0.4.0
// @description  Modern Tampermonkey template for current Chrome/Windows workstations. Replace the placeholder config values before use.
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
// @run-at       document-idle
// ==/UserScript==

(function () {
  'use strict';

  const CONFIG = {
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
    selectors: {
      tableBody: '#verify-account-access-transaction-list > tbody',
      input: '#verify_account_number',
    },
    seenLimit: 200,
    retryIntervalMs: 15000,
    readyPollMs: 3000,
    focusPollMs: 2000,
    showFocusBanner: false,
    debug: false,
  };

  const LOG_PREFIX = 'ASHTON edge';
  const INSTANCE_KEY = '__ashtonEdgeBridgeNodeId__';

  function shouldActivateOnThisPage() {
    if (window.location.protocol === 'file:') {
      return true;
    }

    return window.location.hostname.toLowerCase().includes('touchnet');
  }

  function hasPlaceholderConfig() {
    return [
      CONFIG.baseUrl,
      CONFIG.nodeId,
      CONFIG.token,
      CONFIG.facilityId,
      CONFIG.zoneId,
    ].some((value) => {
      const normalized = String(value).trim();
      return normalized.includes('REPLACE_') || normalized === 'replace-me';
    });
  }

  function debugLog(...args) {
    if (CONFIG.debug) {
      console.info(`${LOG_PREFIX}:`, ...args);
    }
  }

  function warnLog(...args) {
    console.warn(`${LOG_PREFIX}:`, ...args);
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

  const storagePrefix = `ashton.edge.${CONFIG.nodeId.replace(/[^a-z0-9_-]/gi, '_').toLowerCase()}`;

  const state = {
    observer: null,
    readyTimer: null,
    retryTimer: null,
    focusTimer: null,
    banner: null,
    drainingQueue: false,
    pendingEventIds: new Set(),
    seen: loadSeen(),
    queue: loadQueue(),
  };

  function storageKey(suffix) {
    return `${storagePrefix}.${suffix}`;
  }

  function loadSeen() {
    try {
      const raw = window.localStorage.getItem(storageKey('seen'));
      const parsed = JSON.parse(raw || '[]');
      return new Set(Array.isArray(parsed) ? parsed : []);
    } catch (error) {
      warnLog('failed to load seen set', error);
      return new Set();
    }
  }

  function persistSeen() {
    const values = Array.from(state.seen).slice(-CONFIG.seenLimit);
    window.localStorage.setItem(storageKey('seen'), JSON.stringify(values));
  }

  function hasSeen(eventId) {
    return state.seen.has(eventId);
  }

  function rememberEvent(eventId) {
    if (hasSeen(eventId)) {
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
      const parsed = JSON.parse(window.localStorage.getItem(storageKey('queue')) || '[]');
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
    return state.queue.some((queued) => queued.event_id === eventId);
  }

  function queuePayload(payload) {
    if (!queueContains(payload.event_id)) {
      state.queue.push(payload);
      persistQueue();
      warnLog('queued payload for retry', payload.event_id);
    }
  }

  function simpleHash(input) {
    let hash = 2166136261;
    for (let index = 0; index < input.length; index += 1) {
      hash ^= input.charCodeAt(index);
      hash += (hash << 1) + (hash << 4) + (hash << 7) + (hash << 8) + (hash << 24);
    }
    return (`00000000${(hash >>> 0).toString(16)}`).slice(-8);
  }

  function deriveEventId(rowData) {
    const material = [
      CONFIG.nodeId,
      rowData.direction,
      rowData.result,
      (rowData.accountRaw || '').trim(),
      rowData.observedAt,
      rowData.statusMessage || '',
      rowData.accountType || '',
    ].join('|');

    return `edge-${simpleHash(`${material}|a`)}${simpleHash(`${material}|b`)}`;
  }

  function postPayloadWithTampermonkey(url, payload) {
    return new Promise((resolve, reject) => {
      GM_xmlhttpRequest({
        method: 'POST',
        url,
        timeout: 10000,
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
    if (state.drainingQueue || state.queue.length === 0) {
      return;
    }

    state.drainingQueue = true;
    try {
      const remaining = [];
      for (const payload of state.queue) {
        try {
          await postPayload(payload);
          rememberEvent(payload.event_id);
        } catch (error) {
          warnLog('queued retry failed', payload.event_id, error.message || error);
          remaining.push(payload);
        }
      }

      state.queue = remaining;
      persistQueue();
    } finally {
      state.drainingQueue = false;
    }
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

    const eventId = deriveEventId(rowData);
    if (hasSeen(eventId) || queueContains(eventId) || state.pendingEventIds.has(eventId)) {
      return;
    }

    state.pendingEventIds.add(eventId);
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
      await postPayload(payload);
      rememberEvent(eventId);
    } catch (error) {
      warnLog('post failed, queueing for retry', eventId, error.message || error);
      queuePayload(payload);
    } finally {
      state.pendingEventIds.delete(eventId);
    }
  }

  function scanRows(root) {
    if (!root) {
      return;
    }

    if (root instanceof HTMLElement && root.matches('tr')) {
      void processRow(root);
      return;
    }

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
      for (const mutation of mutations) {
        mutation.addedNodes.forEach((node) => {
          if (node instanceof HTMLElement) {
            scanRows(node);
          }
        });
      }
    });

    state.observer.observe(tableBody, { childList: true });
    debugLog('observing table body', CONFIG.selectors.tableBody);
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
    if (!CONFIG.showFocusBanner) {
      return;
    }

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

    if (CONFIG.showFocusBanner) {
      state.focusTimer = window.setInterval(updateFocusBanner, CONFIG.focusPollMs);
    }
  }

  function cleanup() {
    if (state.observer) {
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

  void drainQueue();
  debugLog('script active', window.location.href, CONFIG.nodeId);
  if (!attachObserver()) {
    startPolling();
  } else {
    state.retryTimer = window.setInterval(() => {
      void drainQueue();
    }, CONFIG.retryIntervalMs);
    if (CONFIG.showFocusBanner) {
      state.focusTimer = window.setInterval(updateFocusBanner, CONFIG.focusPollMs);
      updateFocusBanner();
    }
  }
})();
