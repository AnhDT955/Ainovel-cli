import tkinter as tk
from tkinter import ttk, filedialog, messagebox
import subprocess
import threading
import os
import sys

class Txt2EpubApp(tk.Tk):
    def __init__(self):
        super().__init__()
        
        self.title("TXT to EPUB Converter")
        self.geometry("680x480")
        self.configure(bg="#1e1e2e")  # Sleek dark mode background
        
        # Cấu hình Style
        self.setup_styles()
        
        # Giao diện chính
        self.create_widgets()
        
    def setup_styles(self):
        self.style = ttk.Style()
        self.style.theme_use("clam")
        
        # Cấu hình màu sắc dark mode phong cách Catppuccin
        self.style.configure(".", background="#1e1e2e", foreground="#cdd6f4", font=("Segoe UI", 10))
        self.style.configure("TLabel", background="#1e1e2e", foreground="#cdd6f4", font=("Segoe UI", 10, "bold"))
        self.style.configure("TEntry", fieldbackground="#313244", foreground="#cdd6f4", insertcolor="#cdd6f4", borderwidth=0)
        
        # Nút bấm chính màu xanh dương Catppuccin
        self.style.configure("Convert.TButton", 
                             background="#89b4fa", 
                             foreground="#11111b", 
                             font=("Segoe UI", 11, "bold"),
                             borderwidth=0,
                             padding=10)
        self.style.map("Convert.TButton",
                       background=[("active", "#b4befe"), ("pressed", "#74c7ec")])
                       
        # Nút bấm phụ duyệt file
        self.style.configure("Browse.TButton", 
                             background="#45475a", 
                             foreground="#cdd6f4", 
                             font=("Segoe UI", 9, "bold"),
                             borderwidth=0,
                             padding=5)
        self.style.map("Browse.TButton",
                       background=[("active", "#585b70"), ("pressed", "#313244")])

    def create_widgets(self):
        # Header Panel
        header_frame = tk.Frame(self, bg="#181825", height=60)
        header_frame.pack(fill=tk.X, side=tk.TOP)
        header_frame.pack_propagate(False)
        
        header_label = tk.Label(header_frame, text="📚 TXT to EPUB Converter", 
                                font=("Segoe UI", 14, "bold"), bg="#181825", fg="#cdd6f4")
        header_label.pack(side=tk.LEFT, padx=20, pady=15)
        
        # Form Container
        form_frame = tk.Frame(self, bg="#1e1e2e")
        form_frame.pack(fill=tk.BOTH, expand=True, padx=25, pady=20)
        
        # Hàng 0: File đầu vào (Input TXT)
        lbl_input = ttk.Label(form_frame, text="File nguồn TXT:")
        lbl_input.grid(row=0, column=0, sticky=tk.W, pady=8)
        
        self.ent_input = tk.Entry(form_frame, bg="#313244", fg="#cdd6f4", insertbackground="#cdd6f4", 
                                  font=("Segoe UI", 10), bd=1, relief=tk.FLAT)
        self.ent_input.grid(row=0, column=1, sticky=tk.EW, pady=8, padx=(10, 10), ipady=4)
        
        btn_browse_in = ttk.Button(form_frame, text="Duyệt...", style="Browse.TButton", command=self.browse_input)
        btn_browse_in.grid(row=0, column=2, sticky=tk.E, pady=8)
        
        # Hàng 1: File đầu ra (Output EPUB)
        lbl_output = ttk.Label(form_frame, text="Lưu file EPUB:")
        lbl_output.grid(row=1, column=0, sticky=tk.W, pady=8)
        
        self.ent_output = tk.Entry(form_frame, bg="#313244", fg="#cdd6f4", insertbackground="#cdd6f4", 
                                   font=("Segoe UI", 10), bd=1, relief=tk.FLAT)
        self.ent_output.grid(row=1, column=1, sticky=tk.EW, pady=8, padx=(10, 10), ipady=4)
        
        btn_browse_out = ttk.Button(form_frame, text="Lưu tại...", style="Browse.TButton", command=self.browse_output)
        btn_browse_out.grid(row=1, column=2, sticky=tk.E, pady=8)
        
        # Hàng 2: Tiêu đề truyện (Novel Title)
        lbl_title = ttk.Label(form_frame, text="Tên truyện:")
        lbl_title.grid(row=2, column=0, sticky=tk.W, pady=8)
        
        self.ent_title = tk.Entry(form_frame, bg="#313244", fg="#cdd6f4", insertbackground="#cdd6f4", 
                                  font=("Segoe UI", 10), bd=1, relief=tk.FLAT)
        self.ent_title.grid(row=2, column=1, columnspan=2, sticky=tk.EW, pady=8, padx=(10, 0), ipady=4)
        
        # Grid weight config
        form_frame.columnconfigure(1, weight=1)
        
        # Log Output Panel (để xem tiến trình)
        lbl_log = ttk.Label(form_frame, text="Nhật ký xử lý:")
        lbl_log.grid(row=3, column=0, sticky=tk.W, pady=(15, 5))
        
        self.txt_log = tk.Text(form_frame, bg="#11111b", fg="#a6e3a1", font=("Consolas", 9), 
                               height=8, bd=0, relief=tk.FLAT)
        self.txt_log.grid(row=4, column=0, columnspan=3, sticky=tk.NSEW, pady=5)
        self.txt_log.insert(tk.END, "Sẵn sàng.\n")
        self.txt_log.config(state=tk.DISABLED)
        
        form_frame.rowconfigure(4, weight=1)
        
        # Hàng nút Convert
        self.btn_convert = ttk.Button(self, text="⚡ Chuyển đổi sang EPUB", style="Convert.TButton", command=self.start_conversion)
        self.btn_convert.pack(fill=tk.X, side=tk.BOTTOM, padx=25, pady=(0, 25))

    def log(self, message):
        self.txt_log.config(state=tk.NORMAL)
        self.txt_log.insert(tk.END, message + "\n")
        self.txt_log.see(tk.END)
        self.txt_log.config(state=tk.DISABLED)

    def browse_input(self):
        file_path = filedialog.askopenfilename(
            title="Chọn file văn bản TXT nguồn",
            filetypes=[("Text Files", "*.txt"), ("All Files", "*.*")]
        )
        if file_path:
            # Chuẩn hóa đường dẫn
            file_path = os.path.normpath(file_path)
            self.ent_input.delete(0, tk.END)
            self.ent_input.insert(0, file_path)
            
            # Tự động gợi ý đường dẫn EPUB
            dir_name = os.path.dirname(file_path)
            base_name = os.path.splitext(os.path.basename(file_path))[0]
            suggested_out = os.path.join(dir_name, base_name + ".epub")
            
            self.ent_output.delete(0, tk.END)
            self.ent_output.insert(0, suggested_out)
            
            # Tự động gợi ý tên truyện (loại bỏ 《 》 hoặc ký tự đặc biệt nếu có)
            suggested_title = base_name.replace("《", "").replace("》", "").strip()
            self.ent_title.delete(0, tk.END)
            self.ent_title.insert(0, suggested_title)
            
            self.log(f"Đã chọn file nguồn: {file_path}")

    def browse_output(self):
        file_path = filedialog.asksaveasfilename(
            title="Chọn nơi lưu file EPUB",
            defaultextension=".epub",
            filetypes=[("EPUB Books", "*.epub"), ("All Files", "*.*")]
        )
        if file_path:
            file_path = os.path.normpath(file_path)
            self.ent_output.delete(0, tk.END)
            self.ent_output.insert(0, file_path)
            self.log(f"Đường dẫn lưu file: {file_path}")

    def start_conversion(self):
        input_path = self.ent_input.get().strip()
        output_path = self.ent_output.get().strip()
        title = self.ent_title.get().strip()
        
        if not input_path:
            # messagebox warning in Vietnamese
            messagebox.showwarning("Thiếu thông tin", "Vui lòng chọn file nguồn TXT trước.")
            return
            
        self.btn_convert.config(state=tk.DISABLED, text="⏳ Đang chuyển đổi...")
        self.log(f"Bắt đầu chuyển đổi...")
        
        # Chạy trong thread riêng để không bị đơ UI
        threading.Thread(target=self.run_conversion_process, args=(input_path, output_path, title), daemon=True).start()

    def run_conversion_process(self, input_path, output_path, title):
        try:
            # Xác định cách thức gọi txt2epub
            # Ưu tiên sử dụng file txt2epub.exe nếu tồn tại ở thư mục hiện tại
            exe_path = "./txt2epub.exe"
            cmd = []
            
            if os.path.exists(exe_path) or os.path.exists(exe_path + ".exe"):
                cmd = [exe_path]
            else:
                # Nếu không có exe, dùng go run
                cmd = ["go", "run", "cmd/txt2epub/main.go"]
                
            cmd.extend(["-input", input_path])
            if output_path:
                cmd.extend(["-output", output_path])
            if title:
                cmd.extend(["-title", title])
                
            self.log(f"Chạy lệnh: {' '.join(cmd)}")
            
            # Khởi chạy tiến trình
            # Ẩn console window trên Windows nếu dùng exe
            startupinfo = None
            if sys.platform == "win32":
                startupinfo = subprocess.STARTUPINFO()
                startupinfo.dwFlags |= subprocess.STARTF_USESHOWWINDOW
                
            proc = subprocess.Popen(
                cmd,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
                encoding="utf-8",
                startupinfo=startupinfo
            )
            
            stdout, stderr = proc.communicate()
            
            if proc.returncode == 0:
                self.log(stdout.strip())
                self.log("✓ Chuyển đổi hoàn tất thành công!")
                # Hiển thị thông báo trên luồng UI chính
                self.after(0, lambda: messagebox.showinfo("Thành công", f"Đã chuyển đổi thành công sang file EPUB!\nĐường dẫn: {output_path if output_path else input_path + '.epub'}"))
            else:
                self.log(f"LỖI (Mã lỗi {proc.returncode}):")
                self.log(stderr.strip())
                self.after(0, lambda: messagebox.showerror("Lỗi chuyển đổi", f"Chuyển đổi thất bại:\n{stderr.strip()}"))
                
        except Exception as e:
            self.log(f"Ngoại lệ xảy ra: {str(e)}")
            self.after(0, lambda: messagebox.showerror("Lỗi hệ thống", f"Đã xảy ra lỗi ngoài ý muốn:\n{str(e)}"))
            
        finally:
            # Khôi phục trạng thái nút bấm trên luồng UI chính
            self.after(0, lambda: self.btn_convert.config(state=tk.NORMAL, text="⚡ Chuyển đổi sang EPUB"))

if __name__ == "__main__":
    app = Txt2EpubApp()
    app.mainloop()
