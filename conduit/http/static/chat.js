let sessionId = null;
let eventsUrl = null;
let eventSource = null;
let currentAssistantDiv = null;
let accumulatedText = '';
let isTurnInProgress = false;

function setStatus(text) {
    document.getElementById('status').textContent = text || '';
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
            eventsUrl = data.events_url;
            setStatus('Ready');
            connectSSE(eventsUrl);
        })
        .catch(err => {
            setStatus('Error: ' + err.message);
            console.error('Session creation failed:', err);
        });
}

function connectSSE(url) {
    if (eventSource) {
        eventSource.close();
    }
    eventSource = new EventSource(url);

    eventSource.addEventListener('text_delta', (e) => {
        try {
            const data = JSON.parse(e.data);
            renderAssistantDelta(data.content || '');
        } catch (err) {
            console.error('Failed to parse text_delta:', err);
        }
    });

    eventSource.addEventListener('reasoning_delta', (e) => {
        try {
            const data = JSON.parse(e.data);
            renderAssistantDelta(data.content || '');
        } catch (err) {
            console.error('Failed to parse reasoning_delta:', err);
        }
    });

    eventSource.addEventListener('tool_call_delta', (e) => {
        // Tool call deltas carry partial JSON arguments; silently consume.
        try {
            JSON.parse(e.data);
        } catch (err) {
            console.error('Failed to parse tool_call_delta:', err);
        }
    });

    eventSource.addEventListener('turn_complete', () => {
        finalizeTurn();
    });

    eventSource.addEventListener('error', (e) => {
        try {
            const data = JSON.parse(e.data);
            setStatus('Error: ' + (data.message || 'Unknown error'));
        } catch (err) {
            setStatus('Error occurred');
        }
        isTurnInProgress = false;
        updateSendButton();
    });

    eventSource.onerror = (err) => {
        console.error('SSE connection error:', err);
        if (eventSource.readyState !== EventSource.CONNECTING) {
            setStatus('Connection lost');
        }
    };

    eventSource.onopen = () => {
        setStatus(isTurnInProgress ? 'thinking...' : 'Ready');
    };
}

function renderUserMessage(content) {
    const chat = document.getElementById('chat');
    const div = document.createElement('div');
    div.className = 'message user';
    div.textContent = content;
    chat.appendChild(div);
    scrollToBottom();
}

function renderAssistantDelta(content) {
    const chat = document.getElementById('chat');
    if (!currentAssistantDiv) {
        currentAssistantDiv = document.createElement('div');
        currentAssistantDiv.className = 'message assistant';
        chat.appendChild(currentAssistantDiv);
    }
    accumulatedText += content;
    // Show raw text during streaming; markdown is parsed on turn_complete.
    currentAssistantDiv.textContent = accumulatedText;
    scrollToBottom();
}

function finalizeTurn() {
    if (currentAssistantDiv) {
        if (typeof marked !== 'undefined' && accumulatedText) {
            try {
                currentAssistantDiv.innerHTML = marked.parse(accumulatedText);
            } catch (err) {
                console.error('Markdown parsing failed:', err);
                currentAssistantDiv.textContent = accumulatedText;
            }
        }
        currentAssistantDiv = null;
        accumulatedText = '';
    }
    isTurnInProgress = false;
    setStatus('Ready');
    updateSendButton();
}

function sendMessage(content) {
    if (!sessionId || isTurnInProgress) return;

    isTurnInProgress = true;
    setStatus('thinking...');
    updateSendButton();
    renderUserMessage(content);

    fetch('/sessions/' + sessionId + '/messages', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: content })
    }).then(async r => {
        if (!r.ok) {
            throw new Error('Failed to send message (' + r.status + ')');
        }
        // Consume NDJSON body to prevent connection leaks.
        await r.text();
    }).catch(err => {
        setStatus('Error: ' + err.message);
        console.error('Send failed:', err);
        isTurnInProgress = false;
        updateSendButton();
    });
}

function updateSendButton() {
    const btn = document.getElementById('send-btn');
    btn.disabled = isTurnInProgress;
}

function scrollToBottom() {
    const chat = document.getElementById('chat');
    chat.scrollTop = chat.scrollHeight;
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

// Create session on page load.
createSession();
