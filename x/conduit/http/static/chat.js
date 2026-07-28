// Audio notification contract for the HTTP conduit web UI.
// When the server advertises the "audio-notification" capability,
// the client plays a short tone on assistant lifecycle "done"
// (880Hz sine) and a lower buzz on error (220Hz sawtooth).
// AudioContext is created lazily on first user interaction to satisfy
// browser autoplay policies.

let sessionId = null;
let isTurnInProgress = false;
let typingIndicatorDiv = null;
let audioCtx = null;
let lastStatus = {};
let eventsSource = null;

// ensureAudio lazily creates an AudioContext on first use. This avoids
// the autoplay restriction in most browsers and defers resource setup
// until the user has actually interacted with the page.
function ensureAudio() {
    if (!audioCtx && (window.AudioContext || window.webkitAudioContext)) {
        try {
            audioCtx = new (window.AudioContext || window.webkitAudioContext)();
        } catch (e) {
            // Silently fail if audio is not supported or blocked
        }
    }
    return audioCtx;
}

// playTone creates a short beep using the Web Audio API. The gain node
// uses an exponential ramp to avoid audible clicks at the end of the tone.
function playTone(freq, duration, type = 'sine') {
    const ctx = ensureAudio();
    if (!ctx) return;
    try {
        const osc = ctx.createOscillator();
        const gain = ctx.createGain();
        osc.type = type;
        osc.frequency.value = freq;
        osc.connect(gain);
        gain.connect(ctx.destination);
        osc.start();
        gain.gain.exponentialRampToValueAtTime(0.001, ctx.currentTime + duration);
        osc.stop(ctx.currentTime + duration);
    } catch (e) {
        // Silently fail
    }
}

// playDone emits a high-pitch "ding" (880Hz sine) to indicate a
// successful assistant turn.
function playDone() {
    playTone(880, 0.15);
}

// playError emits a low-pitch "buzz" (220Hz sawtooth) to signal an
// error condition.
function playError() {
    playTone(220, 0.3, 'sawtooth');
}

function setStatus(text) {
    document.getElementById('status').textContent = text || '';
}

// openSessionEvents opens an EventSource on /sessions/{id}/events
// and routes the events into handleEvent. The source is closed on
// page unload or when the session changes.
function openSessionEvents(id) {
    if (eventsSource) {
        eventsSource.close();
        eventsSource = null;
    }
    eventsSource = new EventSource('/sessions/' + encodeURIComponent(id) + '/events');
    eventsSource.onmessage = (e) => {
        try {
            const event = JSON.parse(e.data);
            handleEvent(event);
        } catch (err) {
            console.error('Failed to parse SSE event:', err, e.data);
        }
    };
    eventsSource.onerror = (e) => {
        console.warn('SSE error:', e);
    };
}

function createSession() {
    setStatus('Creating session...');
    fetch('/sessions', { method: 'POST' })
        .then(r => {
            if (!r.ok) throw new Error('Failed to create session (' + r.status + ')');
            return r.json();
        })
        .then(data => {
            sessionId = data.id;
            setStatus('Ready');
            openSessionEvents(sessionId);
        })
        .catch(err => {
            setStatus('Error: ' + err.message);
            console.error('Session creation failed:', err);
        });
}

function attachToThread(threadId) {
    setStatus('Attaching to thread...');
    fetch('/sessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ thread_id: threadId })
    })
        .then(r => {
            if (r.status === 404) throw new Error('Thread not found');
            if (!r.ok) throw new Error('Failed to attach (' + r.status + ')');
            return r.json();
        })
        .then(data => {
            sessionId = data.id;
            setStatus('Ready');
            openSessionEvents(sessionId);
        })
        .catch(err => {
            setStatus('Error: ' + err.message);
            console.error('Thread attach failed:', err);
        });
}

function scrollToBottom() {
    const chat = document.getElementById('chat');
    chat.scrollTop = chat.scrollHeight;
}

function renderUserMessage(content) {
    const chat = document.getElementById('chat');
    const div = document.createElement('div');
    div.className = 'message user';
    div.textContent = content;
    chat.appendChild(div);
    scrollToBottom();
}

function showTypingIndicator() {
    if (typingIndicatorDiv) return;
    const chat = document.getElementById('chat');
    typingIndicatorDiv = document.createElement('div');
    typingIndicatorDiv.className = 'message assistant typing';
    const indicator = document.createElement('div');
    indicator.className = 'typing-indicator';
    indicator.textContent = '...';
    typingIndicatorDiv.appendChild(indicator);
    chat.appendChild(typingIndicatorDiv);
    scrollToBottom();
}

function hideTypingIndicator() {
    if (typingIndicatorDiv) {
        typingIndicatorDiv.remove();
        typingIndicatorDiv = null;
    }
}

function renderTextBlock(content) {
    hideTypingIndicator();
    const chat = document.getElementById('chat');
    const div = document.createElement('div');
    div.className = 'message assistant';
    try {
        div.innerHTML = marked.parse(content);
    } catch (err) {
        console.error('Markdown parsing failed:', err);
        div.textContent = content;
    }
    chat.appendChild(div);
    scrollToBottom();
}

function renderReasoningBlock(content) {
    hideTypingIndicator();
    const chat = document.getElementById('chat');
    const div = document.createElement('div');
    div.className = 'message reasoning';
    const details = document.createElement('details');
    const summary = document.createElement('summary');
    summary.textContent = 'Thinking...';
    details.appendChild(summary);
    const contentDiv = document.createElement('div');
    contentDiv.className = 'reasoning-content';
    contentDiv.textContent = content;
    details.appendChild(contentDiv);
    div.appendChild(details);
    chat.appendChild(div);
    scrollToBottom();
}

function renderToolCallBlock(id, name, args, display) {
    hideTypingIndicator();
    const chat = document.getElementById('chat');
    const div = document.createElement('div');
    div.className = 'message tool-call';
    var content;
    if (display) {
        content = '<strong>Tool Call:</strong> ' + escapeHtml(name) +
            ' <span class="tool-id">(' + escapeHtml(id) + ')</span>' +
            '<pre><code>' + escapeHtml(display) + '</code></pre>';
    } else {
        content = '<strong>Tool Call:</strong> ' + escapeHtml(name) +
            ' <span class="tool-id">(' + escapeHtml(id) + ')</span>' +
            '<pre><code>' + escapeHtml(args) + '</code></pre>';
    }
    div.innerHTML = content;
    chat.appendChild(div);
    scrollToBottom();
}

function renderToolResultBlock(toolCallId, content, isError) {
    hideTypingIndicator();
    const chat = document.getElementById('chat');
    const div = document.createElement('div');
    div.className = 'message tool-result' + (isError ? ' error' : '');
    div.innerHTML = '<strong>Tool Result' + (isError ? ' (Error)' : '') + ':</strong> ' +
        '<span class="tool-id">(' + escapeHtml(toolCallId) + ')</span>' +
        '<pre><code>' + escapeHtml(content) + '</code></pre>';
    chat.appendChild(div);
    scrollToBottom();
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function finalizeTurn() {
    hideTypingIndicator();
    isTurnInProgress = false;
    if (lastStatus.thread_id) {
        setStatus(`thread_id=${lastStatus.thread_id}`);
    } else {
        setStatus('Ready');
    }
    updateSendButton();
}

// renderArtifact extracts the inner payload of an artifact event
// and dispatches to the right renderer. The server emits artifact
// events with the artifact's own kind field (text, reasoning, etc.).
function renderArtifact(art) {
    switch (art.kind) {
        case 'text':
            renderTextBlock(art.content);
            return;
        case 'reasoning':
            renderReasoningBlock(art.content);
            return;
        case 'tool_call':
            renderToolCallBlock(art.id, art.name, art.arguments, art.display);
            return;
        case 'tool_result':
            renderToolResultBlock(art.tool_call_id, art.content, art.is_error);
            return;
        case 'usage':
        case 'image':
            // Ignored for chat UI.
            return;
        default:
            console.warn('Unknown artifact kind:', art.kind);
    }
}

function handleEvent(event) {
    // Skip deltas: the block-based UI renders assistant output only
    // at the end of each turn via the turn_complete event.
    if (event.kind === 'text_delta' || event.kind === 'reasoning_delta' ||
        event.kind === 'tool_call_delta' || event.kind === 'tool_result_delta') {
        return;
    }

    if (event.kind === 'turn_complete' && event.turn && event.turn.artifacts) {
        for (const art of event.turn.artifacts) {
            renderArtifact(art);
        }
        return;
    }

    if (event.kind === 'lifecycle') {
        if (event.phase === 'done') {
            playDone();
            finalizeTurn();
        } else if (event.phase === 'cancelled') {
            playError();
            setStatus('Turn cancelled');
            finalizeTurn();
        }
        return;
    }

    if (event.kind === 'error') {
        playError();
        setStatus('Error: ' + (event.message || 'Unknown error'));
        finalizeTurn();
        return;
    }

    if (event.kind === 'properties' && event.operations) {
        const next = Object.assign({}, lastStatus);
        for (const op of event.operations) {
            if (op.op === 'set') next[op.key] = op.value;
            else if (op.op === 'delete') delete next[op.key];
        }
        lastStatus = next;
        const parts = [];
        for (const [key, val] of Object.entries(lastStatus)) {
            if (val) parts.push(`${key}=${val}`);
        }
        setStatus(parts.join(' | ') || '');
        return;
    }

    if (event.kind === 'notice') {
        setStatus(event.content || '');
        return;
    }

    console.warn('Unknown event kind:', event.kind);
}

async function sendMessage(content) {
    ensureAudio();
    if (isTurnInProgress) return;

    isTurnInProgress = true;
    updateSendButton();
    renderUserMessage(content);
    showTypingIndicator();

    try {
        if (!sessionId) {
            const createRes = await fetch('/sessions', { method: 'POST' });
            if (!createRes.ok) {
                throw new Error('Failed to create session (' + createRes.status + ')');
            }
            const createData = await createRes.json();
            sessionId = createData.id;
            history.pushState(null, '', '/chat?thread=' + sessionId);
            openSessionEvents(sessionId);
        }

        const submitRes = await fetch('/sessions/' + encodeURIComponent(sessionId) + '/events', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ kind: 'user_message', content: content })
        });

        if (!submitRes.ok) {
            throw new Error('Failed to send message (' + submitRes.status + ')');
        }

        // Output is delivered via the SSE stream opened in
        // openSessionEvents. We rely on the lifecycle "done" event
        // to finalize the turn.
    } catch (err) {
        setStatus('Error: ' + err.message);
        console.error('Send failed:', err);
        finalizeTurn();
    }
}

function updateSendButton() {
    const btn = document.getElementById('send-btn');
    btn.disabled = isTurnInProgress;
}

function handleSend() {
    const input = document.getElementById('message-input');
    const content = input.value.trim();
    if (!content || isTurnInProgress) return;
    input.value = '';
    resetTextareaHeight();
    sendMessage(content);
}

function resetTextareaHeight() {
    const input = document.getElementById('message-input');
    input.style.height = 'auto';
    input.style.height = Math.min(input.scrollHeight, 128) + 'px';
}

// Event listeners.
document.getElementById('send-btn').addEventListener('click', handleSend);
document.getElementById('message-input').addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        handleSend();
    }
});

// Auto-resize textarea.
document.getElementById('message-input').addEventListener('input', function() {
    this.style.height = 'auto';
    this.style.height = Math.min(this.scrollHeight, 128) + 'px';
});

// Boot: parse URL for ?thread= param and attach, or show ready state.
const threadId = new URLSearchParams(window.location.search).get('thread');
if (threadId) {
    attachToThread(threadId);
} else {
    setStatus('Ready — type a message to start');
}

// Close the SSE stream on page unload so the server-side
// subscriber goroutine can exit promptly.
window.addEventListener('beforeunload', () => {
    if (eventsSource) {
        eventsSource.close();
        eventsSource = null;
    }
});
