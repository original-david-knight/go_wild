#!/usr/bin/env python3
"""
Persistent Python Kernel for GoWild Agent.

This kernel runs as a long-lived subprocess, accepting JSON commands via stdin
and returning JSON results via stdout. State is preserved between executions.

Protocol:
- Input: JSON object with "action" and parameters, terminated by newline
- Output: JSON object with "success", "result"/"error", terminated by newline

Actions:
- execute: Run code, return output and expression result
- get_var: Get a variable's value
- set_var: Set a variable's value
- list_vars: List all user-defined variables
- save_state: Pickle all variables to a file
- load_state: Load variables from a pickle file
- reset: Clear all variables
- ping: Health check
- shutdown: Exit the kernel
"""

import sys
import json
import traceback
import pickle
import io
from contextlib import redirect_stdout, redirect_stderr
from typing import Any

# Global namespace for user code
_user_globals: dict[str, Any] = {
    "__builtins__": __builtins__,
    "__name__": "__main__",
}

# Built-in names to exclude from variable listing
_BUILTIN_NAMES = {"__builtins__", "__name__", "__doc__", "__package__", "__loader__", "__spec__"}


def safe_repr(obj: Any, max_length: int = 10000) -> str:
    """Safely convert object to string representation."""
    try:
        r = repr(obj)
        if len(r) > max_length:
            r = r[:max_length] + "... [truncated]"
        return r
    except Exception as e:
        return f"<repr failed: {e}>"


def execute_code(code: str) -> dict:
    """Execute Python code and capture output."""
    stdout_capture = io.StringIO()
    stderr_capture = io.StringIO()
    result = None

    try:
        with redirect_stdout(stdout_capture), redirect_stderr(stderr_capture):
            # Try to eval as expression first (for REPL-like behavior)
            try:
                result = eval(code, _user_globals)
            except SyntaxError:
                # Not an expression, exec as statements
                exec(code, _user_globals)
                result = None
    except Exception:
        return {
            "success": False,
            "error": traceback.format_exc(),
            "stdout": stdout_capture.getvalue(),
            "stderr": stderr_capture.getvalue(),
        }

    return {
        "success": True,
        "result": safe_repr(result) if result is not None else None,
        "stdout": stdout_capture.getvalue(),
        "stderr": stderr_capture.getvalue(),
    }


def get_variable(name: str) -> dict:
    """Get a variable's value."""
    if name not in _user_globals:
        return {"success": False, "error": f"Variable '{name}' not found"}

    try:
        value = _user_globals[name]
        return {
            "success": True,
            "name": name,
            "value": safe_repr(value),
            "type": type(value).__name__,
        }
    except Exception as e:
        return {"success": False, "error": str(e)}


def set_variable(name: str, value_code: str) -> dict:
    """Set a variable by evaluating code."""
    try:
        value = eval(value_code, _user_globals)
        _user_globals[name] = value
        return {
            "success": True,
            "name": name,
            "value": safe_repr(value),
            "type": type(value).__name__,
        }
    except Exception:
        return {"success": False, "error": traceback.format_exc()}


def list_variables() -> dict:
    """List all user-defined variables."""
    variables = []
    for name, value in _user_globals.items():
        if name in _BUILTIN_NAMES:
            continue
        if callable(value) and hasattr(value, "__module__"):
            # Skip imported functions/classes
            continue
        variables.append({
            "name": name,
            "type": type(value).__name__,
            "value_preview": safe_repr(value, max_length=200),
        })
    return {"success": True, "variables": variables, "count": len(variables)}


def save_state(filepath: str) -> dict:
    """Save all user variables to a pickle file."""
    try:
        # Filter out non-picklable items
        state = {}
        for name, value in _user_globals.items():
            if name in _BUILTIN_NAMES:
                continue
            try:
                pickle.dumps(value)  # Test if picklable
                state[name] = value
            except (pickle.PicklingError, TypeError, AttributeError):
                pass  # Skip non-picklable items

        with open(filepath, "wb") as f:
            pickle.dump(state, f)

        return {"success": True, "saved_count": len(state), "filepath": filepath}
    except Exception:
        return {"success": False, "error": traceback.format_exc()}


def load_state(filepath: str) -> dict:
    """Load variables from a pickle file."""
    try:
        with open(filepath, "rb") as f:
            state = pickle.load(f)

        _user_globals.update(state)
        return {"success": True, "loaded_count": len(state), "filepath": filepath}
    except Exception:
        return {"success": False, "error": traceback.format_exc()}


def reset_state() -> dict:
    """Clear all user-defined variables."""
    to_remove = [k for k in _user_globals if k not in _BUILTIN_NAMES]
    for k in to_remove:
        del _user_globals[k]
    return {"success": True, "cleared_count": len(to_remove)}


def handle_command(cmd: dict) -> dict:
    """Route command to appropriate handler."""
    action = cmd.get("action", "")

    if action == "execute":
        return execute_code(cmd.get("code", ""))
    elif action == "get_var":
        return get_variable(cmd.get("name", ""))
    elif action == "set_var":
        return set_variable(cmd.get("name", ""), cmd.get("value", ""))
    elif action == "list_vars":
        return list_variables()
    elif action == "save_state":
        return save_state(cmd.get("filepath", ""))
    elif action == "load_state":
        return load_state(cmd.get("filepath", ""))
    elif action == "reset":
        return reset_state()
    elif action == "ping":
        return {"success": True, "message": "pong", "session_vars": len(_user_globals) - len(_BUILTIN_NAMES)}
    elif action == "shutdown":
        return {"success": True, "message": "shutting down"}
    else:
        return {"success": False, "error": f"Unknown action: {action}"}


def main():
    """Main REPL loop."""
    # Signal ready
    print(json.dumps({"ready": True}), flush=True)

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue

        try:
            cmd = json.loads(line)
        except json.JSONDecodeError as e:
            print(json.dumps({"success": False, "error": f"Invalid JSON: {e}"}), flush=True)
            continue

        result = handle_command(cmd)
        print(json.dumps(result), flush=True)

        if cmd.get("action") == "shutdown":
            break


if __name__ == "__main__":
    main()
