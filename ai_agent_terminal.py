#!/usr/bin/env python3
"""
AI Agent для работы в терминале с автоматическим выполнением команд.
Использует Ollama для подключения к локальной LLM модели.
Реализует цикл: запрос → планирование → выполнение → результат → ответ
"""

import os
import subprocess
import sys
from typing import Type, List, Optional, Annotated
from pathlib import Path

from langchain_ollama import ChatOllama
from langchain_core.tools import BaseTool, tool
from langchain_core.messages import HumanMessage, AIMessage, SystemMessage, ToolMessage
from langgraph.graph import StateGraph, START, END
from typing_extensions import TypedDict
from langgraph.graph.message import add_messages
from langgraph.prebuilt import ToolNode


# Рабочая директория проекта
WORK_DIR = "/workspace"
DEVAST_DIR = os.path.join(WORK_DIR, "Devast.io.ru-main")


# ==================== ИНСТРУМЕНТЫ ====================

def shell_tool(command: str) -> str:
    """Выполняет команду в оболочке (bash) и возвращает результат.
    Используйте для запуска команд, компиляции кода, проверки файлов.
    
    Args:
        command: Команда для выполнения в bash
    """
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


def read_file_tool(file_path: str) -> str:
    """Читает содержимое файла.
    
    Args:
        file_path: Путь к файлу (относительный или абсолютный)
    """
    try:
        abs_path = os.path.abspath(os.path.join(WORK_DIR, file_path))
        if not abs_path.startswith(WORK_DIR):
            return f"Ошибка: доступ только к файлам в {WORK_DIR}"
        
        if not os.path.exists(abs_path):
            return f"Ошибка: файл не найден: {abs_path}"
        
        with open(abs_path, 'r', encoding='utf-8', errors='ignore') as f:
            content = f.read()
        return f"Содержимое файла {file_path}:\n{content}"
    except Exception as e:
        return f"Ошибка при чтении файла: {str(e)}"


def write_file_tool(file_path: str, content: str) -> str:
    """Записывает содержимое в файл (создает новый или перезаписывает).
    
    Args:
        file_path: Путь к файлу
        content: Содержимое для записи
    """
    try:
        abs_path = os.path.abspath(os.path.join(WORK_DIR, file_path))
        if not abs_path.startswith(WORK_DIR):
            return f"Ошибка: запись только в файлы внутри {WORK_DIR}"
        
        os.makedirs(os.path.dirname(abs_path), exist_ok=True)
        
        with open(abs_path, 'w', encoding='utf-8') as f:
            f.write(content)
        
        return f"✓ Файл успешно записан: {file_path}"
    except Exception as e:
        return f"Ошибка при записи файла: {str(e)}"


def list_directory_tool(dir_path: str = ".") -> str:
    """Показывает список файлов и папок в директории.
    
    Args:
        dir_path: Путь к директории (по умолчанию рабочая директория)
    """
    try:
        abs_path = os.path.abspath(os.path.join(WORK_DIR, dir_path))
        if not abs_path.startswith(WORK_DIR):
            return f"Ошибка: доступ только к директориям в {WORK_DIR}"
        
        if not os.path.isdir(abs_path):
            return f"Ошибка: не является директорией: {abs_path}"
        
        items = []
        for item in sorted(os.listdir(abs_path)):
            full_path = os.path.join(abs_path, item)
            prefix = "[DIR]  " if os.path.isdir(full_path) else "[FILE] "
            items.append(f"{prefix}{item}")
        
        return f"Содержимое {dir_path}:\n" + "\n".join(items)
    except Exception as e:
        return f"Ошибка: {str(e)}"


# Создаем объекты инструментов
tools = [
    tool(shell_tool),
    tool(read_file_tool),
    tool(write_file_tool),
    tool(list_directory_tool),
]


# ==================== АГЕНТ ====================

def create_agent():
    """Создает AI агента с инструментами"""
    
    llm = ChatOllama(
        model="llama3.2",
        temperature=0,
        base_url="http://localhost:11434"
    )
    
    llm_with_tools = llm.bind_tools(tools)
    
    system_message = f"""Ты - автономный AI агент для работы в терминале Linux. 
Твоя задача - выполнять задачи пользователя, самостоятельно используя инструменты.

Доступные инструменты:
1. shell_tool - выполнение bash команд (запуск программ, компиляция, git и т.д.)
2. read_file - чтение файлов (код, конфиги, логи)
3. write_file - запись/изменение файлов (исправление ошибок, создание кода)
4. list_directory - просмотр содержимого директорий

Рабочая директория: {WORK_DIR}
Проект Devast.io.ru-main находится в: {DEVAST_DIR}

ПРАВИЛА РАБОТЫ:
- Всегда сначала анализируй задачу, затем планируй действия
- Используй инструменты последовательно для решения задачи
- После каждого вызова инструмента анализируй результат
- Если команда вернула ошибку - проанализируй и предложи решение
- Для исправления ошибок в main.go: прочитай файл → найди проблему → запиши исправление
- Докладывай пользователю о каждом шаге и конечном результате
- Отвечай на русском языке четко и по делу

Если задача выполнена - сообщи об этом пользователю."""

    return llm_with_tools, system_message


class AgentState(TypedDict):
    messages: Annotated[List, add_messages]


def agent_node(state: AgentState, llm, system_message: str):
    """Узел агента - принимает решение о следующем действии"""
    messages = state["messages"]
    
    # Добавляем системное сообщение в начало если это первый ход
    if len(messages) == 1 or not isinstance(messages[0], SystemMessage):
        messages = [SystemMessage(content=system_message)] + messages
    
    response = llm.invoke(messages)
    return {"messages": [response]}


def should_continue(state: AgentState) -> str:
    """Определяет, продолжать ли выполнение или завершить"""
    messages = state["messages"]
    last_message = messages[-1]
    
    # Если есть вызовы инструментов - продолжаем к их выполнению
    if hasattr(last_message, 'tool_calls') and last_message.tool_calls:
        return "tools"
    
    # Иначе завершаем
    return "end"


def run_agent():
    """Запускает интерактивный цикл агента с автоматическим выполнением"""
    
    print("\n" + "=" * 70)
    print("🤖 AI TERMINAL AGENT - Автономный агент для работы в терминале")
    print("=" * 70)
    print(f"📁 Рабочая директория: {WORK_DIR}")
    print(f"🛠️  Доступные инструменты:")
    print("   • shell_tool     - выполнение bash команд")
    print("   • read_file      - чтение файлов")
    print("   • write_file     - запись/изменение файлов")
    print("   • list_directory - просмотр директорий")
    print("=" * 70)
    print("💡 Примеры запросов:")
    print("   • 'Покажи структуру проекта'")
    print("   • 'Найди ошибки в main.go и исправь их'")
    print("   • 'Скомпилируй проект и запусти тесты'")
    print("   • 'Создай файл test.py с функцией hello world'")
    print("=" * 70)
    print("Команды управления:")
    print("   exit/quit - выход | clear - очистить историю | help - помощь")
    print("=" * 70)
    
    try:
        llm, system_message = create_agent()
    except Exception as e:
        print(f"\n❌ Ошибка инициализации модели: {e}")
        print("\nДля работы агента необходимо:")
        print("1. Установить Ollama: https://ollama.ai")
        print("2. Запустить: ollama serve")
        print("3. Скачать модель: ollama pull llama3.2")
        return
    
    # Создаем граф с циклом выполнения
    workflow = StateGraph(AgentState)
    
    # Добавляем узлы
    workflow.add_node("agent", lambda state: agent_node(state, llm, system_message))
    workflow.add_node("tools", ToolNode(tools))
    
    # Добавляем ребра
    workflow.add_edge(START, "agent")
    workflow.add_conditional_edges(
        "agent",
        should_continue,
        {
            "tools": "tools",
            "end": END
        }
    )
    workflow.add_edge("tools", "agent")
    
    app = workflow.compile()
    
    history = []
    
    while True:
        try:
            user_input = input("\n👤 Вы: ").strip()
            
            if not user_input:
                continue
            
            if user_input.lower() in ['exit', 'quit', 'выход']:
                print("\n👋 До свидания!")
                break
            
            if user_input.lower() == 'clear':
                history = []
                print("🧹 История очищена.")
                continue
            
            if user_input.lower() == 'help':
                print("\n📖 СПРАВКА:")
                print("   Вводите задачи на естественном языке.")
                print("   Агент сам выберет нужные инструменты и выполнит их.")
                print("   Пример: 'Проверь main.go на ошибки и исправь их'")
                continue
            
            # Добавляем сообщение пользователя
            history.append(HumanMessage(content=user_input))
            
            # Запускаем агента
            print("\n🤖 🔄 Агент обрабатывает запрос...")
            print("-" * 70)
            
            step_count = 0
            max_steps = 10  # Ограничение на количество шагов
            
            try:
                config = {"recursion_limit": max_steps * 2}
                result = app.invoke({"messages": history}, config=config)
                
                # Получаем все сообщения
                messages = result["messages"]
                
                # Показываем ход выполнения
                for msg in messages:
                    if isinstance(msg, SystemMessage):
                        continue
                    
                    if isinstance(msg, HumanMessage):
                        # Пропускаем вывод сообщения пользователя (оно уже было введено)
                        continue
                    
                    if isinstance(msg, AIMessage):
                        if hasattr(msg, 'tool_calls') and msg.tool_calls:
                            for tc in msg.tool_calls:
                                print(f"\n🔧 Вызов инструмента: {tc['name']}")
                                args_str = str(tc.get('args', {}))
                                if len(args_str) > 100:
                                    args_str = args_str[:100] + "..."
                                print(f"   Параметры: {args_str}")
                        elif msg.content:
                            print(f"\n💬 Ответ агента:\n{msg.content}")
                    
                    if isinstance(msg, ToolMessage):
                        result_preview = msg.content[:500] if len(msg.content) > 500 else msg.content
                        print(f"\n📊 Результат инструмента:\n{result_preview}")
                        if len(msg.content) > 500:
                            print("... (обрезано)")
                
                # Обновляем историю (добавляем все новые сообщения)
                new_messages = messages[len(history):]
                history.extend(new_messages)
                
            except Exception as e:
                print(f"\n❌ Ошибка при выполнении: {e}")
                # Удаляем последнее сообщение пользователя при ошибке
                if history and isinstance(history[-1], HumanMessage):
                    history.pop()
                
        except KeyboardInterrupt:
            print("\n\n⚠️  Прервано пользователем (Ctrl+C)")
            choice = input("Продолжить работу? (y/n): ").strip().lower()
            if choice != 'y':
                break
        except EOFError:
            print("\n🔚 Конец ввода.")
            break
    
    print("\n✅ Работа агента завершена.\n")


if __name__ == "__main__":
    run_agent()
