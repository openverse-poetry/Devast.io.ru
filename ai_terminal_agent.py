#!/usr/bin/env python3
"""
AI Agent для работы в терминале с возможностью выполнения команд и редактирования файлов.
Использует Ollama для подключения к локальной LLM модели.
"""

import os
import subprocess
import sys
from typing import Type, List, Optional
from pathlib import Path

from langchain_ollama import ChatOllama
from langchain_core.tools import BaseTool, tool
from langchain_core.messages import HumanMessage, AIMessage, SystemMessage
from langgraph.graph import StateGraph, START, END
from typing_extensions import TypedDict, Annotated
from langgraph.graph.message import add_messages


# Рабочая директория проекта
WORK_DIR = "/workspace"
DEVAST_DIR = os.path.join(WORK_DIR, "Devast.io.ru-main")


class ShellInput(BaseTool):
    """Инструмент для выполнения команд в терминале"""
    name: str = "shell_tool"
    description: str = """Выполняет команду в оболочке (bash) и возвращает результат.
    Используйте для запуска команд, компиляции кода, проверки файлов и т.д.
    Входные данные: команда для выполнения."""
    
    def _run(self, command: str) -> str:
        try:
            result = subprocess.run(
                command,
                shell=True,
                capture_output=True,
                text=True,
                timeout=60,
                cwd=WORK_DIR
            )
            output = f"STDOUT:\n{result.stdout}\nSTDERR:\n{result.stderr}\nRETURN CODE: {result.returncode}"
            return output
        except subprocess.TimeoutExpired:
            return "Ошибка: команда выполнялась слишком долго (>60 сек)"
        except Exception as e:
            return f"Ошибка при выполнении команды: {str(e)}"


class ReadFileInput(BaseTool):
    """Инструмент для чтения файлов"""
    name: str = "read_file"
    description: str = """Читает содержимое файла. 
    Используйте для просмотра кода, конфигураций, логов.
    Входные данные: путь к файлу (относительный или абсолютный)."""
    
    def _run(self, file_path: str) -> str:
        try:
            # Проверяем, что файл находится в разрешенной директории
            abs_path = os.path.abspath(os.path.join(WORK_DIR, file_path))
            if not (abs_path.startswith(WORK_DIR)):
                return f"Ошибка: доступ только к файлам в {WORK_DIR}"
            
            if not os.path.exists(abs_path):
                return f"Ошибка: файл не найден: {abs_path}"
            
            with open(abs_path, 'r', encoding='utf-8', errors='ignore') as f:
                content = f.read()
            return f"Содержимое файла {file_path}:\n{content}"
        except Exception as e:
            return f"Ошибка при чтении файла: {str(e)}"


class WriteFileInput(BaseTool):
    """Инструмент для записи/изменения файлов"""
    name: str = "write_file"
    description: str = """Записывает содержимое в файл (создает новый или перезаписывает существующий).
    Используйте для исправления ошибок в коде, создания новых файлов.
    Входные данные: JSON объект с полями 'file_path' (путь к файлу) и 'content' (содержимое)."""
    
    def _run(self, file_path: str, content: str) -> str:
        try:
            abs_path = os.path.abspath(os.path.join(WORK_DIR, file_path))
            if not (abs_path.startswith(WORK_DIR)):
                return f"Ошибка: запись только в файлы внутри {WORK_DIR}"
            
            # Создаем директорию если не существует
            os.makedirs(os.path.dirname(abs_path), exist_ok=True)
            
            with open(abs_path, 'w', encoding='utf-8') as f:
                f.write(content)
            
            return f"Файл успешно записан: {file_path}"
        except Exception as e:
            return f"Ошибка при записи файла: {str(e)}"


class ListDirectoryInput(BaseTool):
    """Инструмент для просмотра содержимого директории"""
    name: str = "list_directory"
    description: str = """Показывает список файлов и папок в указанной директории.
    Входные данные: путь к директории (по умолчанию рабочая директория)."""
    
    def _run(self, dir_path: str = ".") -> str:
        try:
            abs_path = os.path.abspath(os.path.join(WORK_DIR, dir_path))
            if not (abs_path.startswith(WORK_DIR)):
                return f"Ошибка: доступ только к директориям в {WORK_DIR}"
            
            if not os.path.isdir(abs_path):
                return f"Ошибка: не является директорией: {abs_path}"
            
            items = os.listdir(abs_path)
            result = []
            for item in sorted(items):
                full_path = os.path.join(abs_path, item)
                if os.path.isdir(full_path):
                    result.append(f"[DIR]  {item}/")
                else:
                    result.append(f"[FILE] {item}")
            
            return f"Содержимое {dir_path}:\n" + "\n".join(result)
        except Exception as e:
            return f"Ошибка при чтении директории: {str(e)}"


def create_agent():
    """Создает AI агента с инструментами"""
    
    # Инициализация модели через Ollama
    llm = ChatOllama(
        model="llama3.2",  # Или другая доступная модель: "mistral", "codellama", etc.
        temperature=0,
        base_url="http://localhost:11434"
    )
    
    # Создаем инструменты
    tools = [
        ShellInput(),
        ReadFileInput(),
        WriteFileInput(),
        ListDirectoryInput()
    ]
    
    # Добавляем инструменты к модели
    llm_with_tools = llm.bind_tools(tools)
    
    # Системное сообщение
    system_message = f"""Ты - AI агент для работы в терминале Linux. Твоя задача - помогать пользователю выполнять задачи в проекте.

У тебя есть доступ к следующим инструментам:
1. shell_tool - выполнение команд bash в терминале
2. read_file - чтение файлов
3. write_file - запись/изменение файлов  
4. list_directory - просмотр содержимого директорий

Рабочая директория проекта: {WORK_DIR}

Важные правила:
- Ты можешь самостоятельно выполнять команды и исправлять ошибки в коде
- При работе с файлами используй относительные пути от рабочей директории
- Если пользователь просит исправить ошибку в main.go, сначала прочитай файл, пойми проблему, затем предложи исправление
- Всегда докладывай о результатах выполнения команд
- Если команда вернула ошибку, проанализируй её и предложи решение
- Для проекта Devast.io.ru-main проверяй файлы в соответствующей поддиректории

Отвечай на русском языке. Будь конкретным и полезным."""

    return llm_with_tools, tools, system_message


class AgentState(TypedDict):
    messages: Annotated[List, add_messages]


def agent_node(state: AgentState, llm, system_message: str):
    """Основной узел агента"""
    messages = state["messages"]
    
    # Добавляем системное сообщение если это первый запрос
    if len(messages) == 1 or not isinstance(messages[0], SystemMessage):
        messages = [SystemMessage(content=system_message)] + messages
    
    response = llm.invoke(messages)
    return {"messages": [response]}


def run_agent():
    """Запускает интерактивный цикл агента"""
    
    print("=" * 60)
    print("AI Terminal Agent запущен!")
    print("=" * 60)
    print(f"Рабочая директория: {WORK_DIR}")
    print("Доступные команды:")
    print("  - Введите ваш запрос на естественном языке")
    print("  - 'exit' или 'quit' для выхода")
    print("  - 'clear' для очистки истории")
    print("=" * 60)
    
    try:
        llm, tools, system_message = create_agent()
    except Exception as e:
        print(f"Ошибка инициализации модели: {e}")
        print("Убедитесь, что Ollama запущен: ollama serve")
        print("И установите модель: ollama pull llama3.2")
        return
    
    # Создаем граф
    workflow = StateGraph(AgentState)
    workflow.add_node("agent", lambda state: agent_node(state, llm, system_message))
    workflow.add_edge(START, "agent")
    workflow.add_edge("agent", END)
    app = workflow.compile()
    
    history = []
    
    while True:
        try:
            user_input = input("\n👤 Вы: ").strip()
            
            if not user_input:
                continue
            
            if user_input.lower() in ['exit', 'quit', 'выход']:
                print("До свидания!")
                break
            
            if user_input.lower() == 'clear':
                history = []
                print("История очищена.")
                continue
            
            # Добавляем сообщение пользователя
            history.append(HumanMessage(content=user_input))
            
            # Запускаем агента
            print("\n🤖 Агент думает...")
            
            try:
                result = app.invoke({"messages": history})
                response = result["messages"][-1]
                
                # Добавляем ответ агента в историю
                history.append(response)
                
                # Выводим ответ
                print("\n🤖 Агент:", end=" ")
                
                # Проверяем, есть ли вызовы инструментов
                if hasattr(response, 'tool_calls') and response.tool_calls:
                    for tool_call in response.tool_calls:
                        print(f"\n  → Вызывает инструмент: {tool_call.get('name', 'unknown')}")
                        print(f"    Аргументы: {tool_call.get('args', {})}")
                
                if hasattr(response, 'content') and response.content:
                    print(response.content)
                
            except Exception as e:
                print(f"Ошибка при выполнении: {e}")
                # Очищаем историю при ошибке
                history = history[:-1]
                
        except KeyboardInterrupt:
            print("\n\nПрервано пользователем.")
            break
        except EOFError:
            print("\nКонец ввода.")
            break


if __name__ == "__main__":
    run_agent()
