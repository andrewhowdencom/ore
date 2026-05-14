let sessionId = null;
let isTurnInProgress = false;
let typingIndicatorDiv = null;

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

function renderToolCallBlock(id, name, args) {
    hideTypingIndicator();
    const chat = document.getElementById('chat');
    const div = document.createElement('div');
    div.className = 'message tool-call';
    div.innerHTML = '<strong>Tool Call:</strong> ' + escapeHtml(name) +
        ' <span class="tool-id">(' + escapeHtml(id) + ')</span>' +
        '<pre><code>' + escapeHtml(args) + '</code></pre>';
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
    setStatus('Ready');
    updateSendButton();
}

function handleEvent(event) {
    if (event.kind === 'tool_call_delta' || event.kind === 'complete') {
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
        renderToolCallBlock(event.id, event.name, event.arguments);
        return;
    }

    if (event.kind === 'tool_result') {
        renderToolResultBlock(event.tool_call_id, event.content, event.is_error);
        return;
    }

    if (event.kind === 'turn_complete') {
        finalizeTurn();
        return;
    }

    if (event.kind === 'error') {
        setStatus('Error: ' + (event.message || 'Unknown error'));
        finalizeTurn();
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
    if (!sessionId || isTurnInProgress) return;

    isTurnInProgress = true;
    setStatus('thinking...');
    updateSendButton();
    renderUserMessage(content);
    showTypingIndicator();

    try {
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

        finalizeTurn();
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

// Create session on page load.
createSession();
