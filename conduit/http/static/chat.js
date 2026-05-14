let sessionId = null;
let isTurnInProgress = false;
let currentAssistantMessageDiv = null;
let currentBlockKind = null;
let currentBlockContent = '';

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

function renderUserMessage(content) {
    const chat = document.getElementById('chat');
    const div = document.createElement('div');
    div.className = 'message user';
    div.textContent = content;
    chat.appendChild(div);
    scrollToBottom();
}

function renderBlock(kind, content) {
    if (kind === 'reasoning' || kind === 'reasoning_delta') {
        const details = document.createElement('details');
        details.innerHTML = '<summary>Thinking...</summary><div class="reasoning-content"></div>';
        details.querySelector('.reasoning-content').textContent = content;
        return details;
    }
    const div = document.createElement('div');
    try {
        div.innerHTML = marked.parse(content);
    } catch (err) {
        console.error('Markdown parsing failed:', err);
        div.textContent = content;
    }
    return div;
}

function completeCurrentBlock() {
    if (!currentAssistantMessageDiv || !currentBlockKind) return;

    const indicator = currentAssistantMessageDiv.querySelector('.typing-indicator');
    if (indicator) {
        indicator.remove();
    }

    const blockEl = renderBlock(currentBlockKind, currentBlockContent);
    currentAssistantMessageDiv.appendChild(blockEl);

    const newIndicator = document.createElement('div');
    newIndicator.className = 'typing-indicator';
    newIndicator.textContent = '...';
    currentAssistantMessageDiv.appendChild(newIndicator);

    scrollToBottom();

    currentBlockKind = null;
    currentBlockContent = '';
}

function finalizeTurn() {
    if (!currentAssistantMessageDiv) return;

    if (currentBlockKind) {
        const indicator = currentAssistantMessageDiv.querySelector('.typing-indicator');
        if (indicator) {
            indicator.remove();
        }
        const blockEl = renderBlock(currentBlockKind, currentBlockContent);
        currentAssistantMessageDiv.appendChild(blockEl);
        currentBlockKind = null;
        currentBlockContent = '';
    }

    const indicator = currentAssistantMessageDiv.querySelector('.typing-indicator');
    if (indicator) {
        indicator.remove();
    }

    // Remove empty assistant messages (no blocks rendered).
    if (currentAssistantMessageDiv.children.length === 0) {
        currentAssistantMessageDiv.remove();
    }

    currentAssistantMessageDiv = null;
    isTurnInProgress = false;
    setStatus('Ready');
    updateSendButton();
}

function handleEvent(event) {
    if (event.kind === 'tool_call_delta' || event.kind === 'complete') {
        return;
    }

    if (event.kind === 'text_delta' || event.kind === 'reasoning_delta') {
        if (!currentAssistantMessageDiv) {
            const chat = document.getElementById('chat');
            currentAssistantMessageDiv = document.createElement('div');
            currentAssistantMessageDiv.className = 'message assistant';
            const indicator = document.createElement('div');
            indicator.className = 'typing-indicator';
            indicator.textContent = '...';
            currentAssistantMessageDiv.appendChild(indicator);
            chat.appendChild(currentAssistantMessageDiv);
            scrollToBottom();
        }

        const content = event.content || '';
        if (!currentBlockKind) {
            currentBlockKind = event.kind;
            currentBlockContent = content;
        } else if (currentBlockKind === event.kind) {
            currentBlockContent += content;
        } else {
            completeCurrentBlock();
            currentBlockKind = event.kind;
            currentBlockContent = content;
        }
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

    const chat = document.getElementById('chat');
    currentAssistantMessageDiv = document.createElement('div');
    currentAssistantMessageDiv.className = 'message assistant';

    const indicator = document.createElement('div');
    indicator.className = 'typing-indicator';
    indicator.textContent = '...';
    currentAssistantMessageDiv.appendChild(indicator);

    chat.appendChild(currentAssistantMessageDiv);
    scrollToBottom();

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
