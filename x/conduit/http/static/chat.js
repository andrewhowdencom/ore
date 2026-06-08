// Audio notification contract for the HTTP conduit web UI.
// When the server advertises the "audio-notification" capability,
// the client plays a short tone on assistant turn_complete (880Hz sine)
// and a lower buzz on error (220Hz sawtooth). AudioContext is created
// lazily on first user interaction to satisfy browser autoplay policies.

let sessionId = null;
let isTurnInProgress = false;
let typingIndicatorDiv = null;
let audioCtx = null;
let lastStatus = {};

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

function fetchHistory(sessionId) {
    return fetch('/sessions/' + sessionId + '/turns')
        .then(r => {
            if (!r.ok) throw new Error('Failed to fetch history (' + r.status + ')');
            return r.json();
        })
        .then(turns => {
            const chat = document.getElementById('chat');
            chat.innerHTML = '';
            for (const turn of turns) {
                if (turn.role === 'user') {
                    for (const artifact of turn.artifacts) {
                        if (artifact.kind === 'text') {
                            renderUserMessage(artifact.content);
                        }
                    }
                } else if (turn.role === 'assistant') {
                    for (const artifact of turn.artifacts) {
                        if (artifact.kind === 'text') {
                            renderTextBlock(artifact.content);
                        } else if (artifact.kind === 'reasoning') {
                            renderReasoningBlock(artifact.content);
                        } else if (artifact.kind === 'tool_call') {
                            renderToolCallBlock(artifact.id, artifact.name, artifact.arguments, artifact.display);
                        } else if (artifact.kind === 'tool_result') {
                            renderToolResultBlock(artifact.tool_call_id, artifact.content, artifact.is_error);
                        }
                    }
                }
            }
        })
        .catch(err => {
            console.error('History fetch failed:', err);
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
            return fetchHistory(sessionId);
        })
        .catch(err => {
            setStatus('Error: ' + err.message);
            console.error('Thread attach failed:', err);
        });
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
        })
        .catch(err => {
            setStatus('Error: ' + err.message);
            console.error('Session creation failed:', err);
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

function handleEvent(event) {
    if (event.kind === 'tool_call_delta') {
        return;
    }

    if (event.kind === 'text_delta' || event.kind === 'reasoning_delta') {
        // Deltas are not used in the block-based UI.
        return;
    }

    if (event.kind === 'text') {
        renderTextBlock(event.content);
        return;
    }

    if (event.kind === 'reasoning') {
        renderReasoningBlock(event.content);
        return;
    }

    if (event.kind === 'tool_call') {
        renderToolCallBlock(event.id, event.name, event.arguments, event.display);
        return;
    }

    if (event.kind === 'tool_result') {
        renderToolResultBlock(event.tool_call_id, event.content, event.is_error);
        return;
    }

    if (event.kind === 'turn_complete') {
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

    if (event.kind === 'status') {
        Object.assign(lastStatus, event.status);
        const parts = [];
        for (const [key, val] of Object.entries(lastStatus)) {
            if (val) parts.push(`${key}=${val}`);
        }
        setStatus(parts.join(' | ') || '');
        return;
    }

    if (event.kind === 'usage' || event.kind === 'image') {
        // Silently ignore usage and image events in the chat UI.
        return;
    }

    console.warn('Unknown event kind:', event.kind);
}

async function readNDJSONStream(reader, decoder) {
    let buffer = '';

    while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop();

        for (const line of lines) {
            const trimmed = line.trim();
            if (!trimmed) continue;
            try {
                const event = JSON.parse(trimmed);
                handleEvent(event);
            } catch (err) {
                console.error('Failed to parse NDJSON line:', err, line);
            }
        }
    }

    if (buffer.trim()) {
        try {
            const event = JSON.parse(buffer.trim());
            handleEvent(event);
        } catch (err) {
            console.error('Failed to parse final NDJSON line:', err, buffer);
        }
    }
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
            setStatus('Ready');
        }

        const response = await fetch('/sessions/' + sessionId + '/messages', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ content: content })
        });

        if (!response.ok) {
            throw new Error('Failed to send message (' + response.status + ')');
        }

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        await readNDJSONStream(reader, decoder);
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
